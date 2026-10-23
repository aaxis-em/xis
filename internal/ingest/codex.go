package ingest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

func CodexHome() string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
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
