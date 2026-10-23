package ingest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aaxis-em/xis/internal/adapters/claudecode"
	"github.com/aaxis-em/xis/internal/adapters/codex"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/pathx"
	"github.com/aaxis-em/xis/internal/snapshot"
	"github.com/aaxis-em/xis/internal/store"
)

// ImportCodex imports one Codex rollout file. repoDir is used when the
// session's cwd is not a git repository on this machine; base overrides the
// base commit that the rollout recorded.
func (in *Ingester) ImportCodex(path, repoDir, base string) (Result, error) {
	res, err := codex.Parse(path)
	if err != nil {
		return Result{}, err
	}
	tr := &res.Trace
	repo, err := snapshot.Discover(res.Cwd)
	if err != nil || res.Cwd == "" {
		if repoDir == "" {
			return Result{}, fmt.Errorf("the session cwd %q is not a git repository here; use --repo", res.Cwd)
		}
		if repo, err = snapshot.Discover(repoDir); err != nil {
			return Result{}, err
		}
	}
	cwdRel := ""
	if res.Cwd != "" {
		if rel, ok := pathx.Rel(repo.Top, res.Cwd); ok && rel != "." {
			cwdRel = rel
		}
	}
	for i := range tr.Edits {
		tr.Edits[i].FilePath = repoPath(tr.Edits[i].FilePath, res.Cwd, cwdRel, repo.Top)
		if tr.Edits[i].OldPath != "" {
			tr.Edits[i].OldPath = repoPath(tr.Edits[i].OldPath, res.Cwd, cwdRel, repo.Top)
		}
	}

	switch {
	case base != "":
		c, err := repo.Git().Run("rev-parse", "--verify", "-q", base+"^{commit}")
		if err != nil {
			return Result{}, fmt.Errorf("unknown base revision %q", base)
		}
		res.BaseCommit = c
	case res.BaseCommit != "":
		if _, err := repo.Git().Run("cat-file", "-e", res.BaseCommit+"^{commit}"); err != nil {
			tr.Warnings = append(tr.Warnings, "the recorded base commit "+res.BaseCommit+" is not in the repository; using HEAD")
			res.BaseCommit = repo.Head()
		}
	default:
		tr.Warnings = append(tr.Warnings, "the rollout has no base commit; using HEAD (use --base)")
		res.BaseCommit = repo.Head()
	}
	tr.Session.RepoID, tr.Session.BaseCommit = repo.ID, res.BaseCommit
	if existing, err := in.Store.FindSession(codex.Agent, tr.Session.AgentSessionID); err == nil {
		remap(tr, existing.ID)
	} else if !errors.Is(err, store.ErrNotFound) {
		return Result{}, err
	}

	in.redactTrace(tr)
	for i := range tr.Edits {
		tr.Edits[i].Patch = in.redactor.String(tr.Edits[i].Patch)
	}
	testRuns := in.detectTests(tr)
	r := model.Repo{ID: repo.ID, Path: repo.Top, RemoteURL: res.RemoteURL, CreatedAt: tr.Session.StartedAt}
	err = in.Store.Tx(func(tx *store.Store) error {
		if err := tx.PutRepo(r); err != nil {
			return err
		}
		if err := tx.PutSession(tr.Session); err != nil {
			return err
		}
		if err := tx.DeleteSessionData(tr.Session.ID); err != nil {
			return err
		}
		for _, a := range tr.Actors {
			if err := tx.PutActor(a); err != nil {
				return err
			}
		}
		for _, s := range tr.Steps {
			if err := tx.PutStep(s); err != nil {
				return err
			}
		}
		for _, tc := range tr.ToolCalls {
			if err := tx.PutToolCall(tc); err != nil {
				return err
			}
		}
		for _, e := range tr.Edits {
			if err := tx.PutEdit(e); err != nil {
				return err
			}
		}
		for _, t := range testRuns {
			if err := tx.PutTestRun(t); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return Result{SessionID: tr.Session.ID, AgentSessionID: tr.Session.AgentSessionID, Steps: len(tr.Steps), Edits: len(tr.Edits), TestRuns: len(testRuns), Warnings: tr.Warnings}, nil
}

func repoPath(p, cwd, cwdRel, top string) string {
	if rel, ok := pathx.Rel(top, p); ok {
		return rel
	}
	if cwd != "" {
		if rel, ok := pathx.Rel(cwd, p); ok {
			p = rel
		}
	}
	return filepath.ToSlash(filepath.Join(cwdRel, p))
}

// remap keeps the ID of a session that was imported before.
func remap(tr *model.Trace, id string) {
	old := tr.Session.ID
	if old == id {
		return
	}
	tr.Session.ID = id
	for i := range tr.Actors {
		tr.Actors[i].SessionID = id
	}
	for i := range tr.Steps {
		tr.Steps[i].SessionID = id
	}
}

func FindRollouts(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(filepath.Join(root, "sessions"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return filepath.SkipAll
			}
			return err
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), "rollout-") && strings.HasSuffix(d.Name(), ".jsonl") {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

// liveCodex builds a captured Codex session. Codex hooks have the payloads of
// Claude Code hooks, so the hook events give the prompts, the tool calls and
// the snapshots. The rollout adds the agent text, the reasoning, the exit
// codes and the token counts.
func liveCodex(input claudecode.Input, rollout string) model.Trace {
	ro, err := (*codex.Result)(nil), errors.New("no rollout path in the hook events")
	if rollout != "" {
		ro, err = codex.Parse(rollout)
	}
	input.HasTranscript = err == nil
	tr := claudecode.Build(input)
	tr.Session.Agent = codex.Agent
	if err != nil {
		tr.Warnings = append(tr.Warnings, "rollout not read: "+err.Error())
	}
	byCall := map[string]model.ToolCall{}
	used := map[string]bool{}
	if ro != nil {
		for _, tc := range ro.Trace.ToolCalls {
			if tc.ToolUseID != "" {
				byCall[tc.ToolUseID] = tc
			}
			// In code mode, Codex logs one exec call that runs a script; the
			// hooks report the tools that the script calls.
			if tc.ToolName == "exec" {
				used[tc.StepID] = true
			}
		}
	}
	summary := map[string]string{}
	for i := range tr.ToolCalls {
		tc := &tr.ToolCalls[i]
		tc.Category = codex.Category(tc.ToolName)
		// Codex sends no failure event, so only the rollout has the exit code.
		tc.ExitCode = nil
		if rc, ok := byCall[tc.ToolUseID]; ok && tc.ToolUseID != "" {
			tc.Output, tc.ExitCode, tc.OK = rc.Output, rc.ExitCode, rc.OK
			used[rc.StepID] = true
		}
		summary[tc.StepID] = codex.Summary(tc.ToolName, tc.Input)
	}
	for i := range tr.Steps {
		if s, ok := summary[tr.Steps[i].ID]; ok {
			tr.Steps[i].Text = s
		}
	}
	if ro == nil {
		return tr
	}
	s := &tr.Session
	s.AgentVersion, s.Models = ro.Trace.Session.AgentVersion, ro.Trace.Session.Models
	s.InputTokens, s.OutputTokens = ro.Trace.Session.InputTokens, ro.Trace.Session.OutputTokens
	agentActor := tr.Actors[0].ID // Build puts the agent first.
	for _, st := range ro.Trace.Steps {
		// The prompts and the hooked tool calls are steps already.
		if st.Kind == model.StepUserMessage || used[st.ID] {
			continue
		}
		st.SessionID, st.ActorID = s.ID, agentActor
		tr.Steps = append(tr.Steps, st)
	}
	for _, tc := range ro.Trace.ToolCalls {
		if !used[tc.StepID] {
			tr.ToolCalls = append(tr.ToolCalls, tc)
		}
	}
	sort.SliceStable(tr.Steps, func(i, j int) bool { return tr.Steps[i].TsStart.Before(tr.Steps[j].TsStart) })
	for i := range tr.Steps {
		tr.Steps[i].Seq = i + 1
	}
	return tr
}
