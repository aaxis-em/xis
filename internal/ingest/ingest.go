package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aaxis-em/xis/internal/adapters"
	"github.com/aaxis-em/xis/internal/adapters/claudecode"
	"github.com/aaxis-em/xis/internal/adapters/codex"
	"github.com/aaxis-em/xis/internal/capture"
	"github.com/aaxis-em/xis/internal/config"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/redact"
	"github.com/aaxis-em/xis/internal/snapshot"
	"github.com/aaxis-em/xis/internal/store"
	"github.com/aaxis-em/xis/internal/testdetect"
)

type Ingester struct {
	Paths    config.Paths
	Cfg      config.Config
	Store    *store.Store
	redactor *redact.Redactor
	tests    *testdetect.Detector
}

func New(p config.Paths, c config.Config, st *store.Store) (*Ingester, error) {
	r, err := redact.New(c.Redact)
	if err != nil {
		return nil, err
	}
	return &Ingester{Paths: p, Cfg: c, Store: st, redactor: r, tests: testdetect.New(c.Capture.TestGlobs, c.Capture.TestCommands)}, nil
}

type Result struct {
	SessionID      string
	AgentSessionID string
	Steps          int
	Edits          int
	TestRuns       int
	Warnings       []string
}

func eventKey(l capture.Line) string {
	h := sha256.New()
	h.Write([]byte(l.Ts.UTC().Format(time.RFC3339Nano)))
	h.Write([]byte{0})
	h.Write([]byte(l.Event))
	h.Write([]byte{0})
	h.Write(l.Payload)
	return hex.EncodeToString(h.Sum(nil)[:12])
}

// IngestSpool ingests one Claude Code or Codex session from its spool file
// and the events already stored for it, then deletes the spool file.
func (in *Ingester) IngestSpool(agentSessionID string) (Result, error) {
	run, err := capture.LockIngest(in.Paths.Spool, agentSessionID, 2*time.Minute)
	if err != nil {
		return Result{}, fmt.Errorf("another ingest of session %s runs: %w", agentSessionID, err)
	}
	defer run.Release()
	lines, size, err := capture.TakeSpool(in.Paths.Spool, agentSessionID)
	if err != nil {
		return Result{}, err
	}
	res, err := in.ingestLines(agentSessionID, lines)
	if err != nil {
		return res, err
	}
	if err := capture.DropSpool(in.Paths.Spool, agentSessionID, size); err != nil {
		slog.Warn("drop spool", "session", agentSessionID, "err", err.Error())
	}
	return res, nil
}

func (in *Ingester) ingestLines(agentSessionID string, lines []capture.Line) (Result, error) {
	agent := claudecode.Agent
	for _, l := range lines {
		if l.Agent != "" {
			agent = l.Agent
		}
	}
	// Without new lines, only the stored session tells the agent.
	if _, err := in.Store.FindSession(codex.Agent, agentSessionID); len(lines) == 0 && err == nil {
		agent = codex.Agent
	}
	existing, err := in.Store.FindSession(agent, agentSessionID)
	known := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return Result{}, err
	}

	var events []model.HookEvent
	var repoInfo *capture.RepoInfo
	noGit := false
	if known {
		events, err = in.Store.ListHookEvents(existing.ID)
		if err != nil {
			return Result{}, err
		}
	}
	seen := map[string]bool{}
	for _, e := range events {
		seen[e.Key] = true
	}
	for _, l := range lines {
		if l.Repo != nil && (repoInfo == nil || (repoInfo.Head == "" && l.Repo.Head != "")) {
			r := *l.Repo
			repoInfo = &r
		}
		if l.NoGit {
			noGit = true
		}
		key := eventKey(l)
		if seen[key] {
			continue
		}
		seen[key] = true
		events = append(events, model.HookEvent{Key: key, Seq: len(events), Ts: l.Ts, Event: l.Event, Payload: string(l.Payload), Snapshot: l.SnapshotID()})
	}
	if len(events) == 0 {
		return Result{}, fmt.Errorf("session %s has no events", agentSessionID)
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].Seq < events[j].Seq })

	transcriptPath, agentTranscripts := transcriptPaths(events)
	input := claudecode.Input{Events: events}
	if known {
		input.SessionID = existing.ID
	}
	var tr model.Trace
	if agent == codex.Agent {
		tr = liveCodex(input, transcriptPath)
	} else {
		var warnings []string
		if transcriptPath != "" {
			entries, warns, err := claudecode.ReadTranscript(transcriptPath)
			if err == nil {
				input.Transcript, input.HasTranscript = entries, true
				warnings = append(warnings, warns...)
			} else {
				warnings = append(warnings, "transcript not read: "+err.Error())
			}
			input.Subagents = readSubagents(transcriptPath, agentTranscripts, &warnings)
		}
		tr = claudecode.Build(input)
		tr.Warnings = append(tr.Warnings, warnings...)
	}
	tr.Session.NoGit = noGit && len(tr.Snapshots) == 0

	var repo model.Repo
	if repoInfo != nil {
		tr.Session.RepoID = repoInfo.ID
		tr.Session.BaseCommit = repoInfo.Head
		repo = model.Repo{ID: repoInfo.ID, Path: repoInfo.Top, RemoteURL: repoInfo.Remote, CreatedAt: tr.Session.StartedAt}
	} else if known {
		tr.Session.RepoID, tr.Session.BaseCommit = existing.RepoID, existing.BaseCommit
	}
	// The base is the HEAD at the first start of the session. Claude Code
	// sends SessionStart again on resume and after compaction, with a later
	// HEAD, and that must not move the base.
	if known && existing.BaseCommit != "" {
		tr.Session.BaseCommit = existing.BaseCommit
	}

	in.redactTrace(&tr)
	if repoInfo == nil && known && existing.RepoID != "" {
		if r, err := in.Store.GetRepo(existing.RepoID); err == nil && r.Path != "" {
			repoInfo = &capture.RepoInfo{ID: r.ID, Top: r.Path, CommonDir: filepath.Join(r.Path, ".git")}
		}
	}
	if repoInfo != nil && len(tr.Snapshots) > 0 {
		r, err := snapshot.Discover(repoInfo.Top)
		if err != nil {
			r = snapshot.Repo{Top: repoInfo.Top, CommonDir: repoInfo.CommonDir, ObjectsDir: filepath.Join(repoInfo.CommonDir, "objects"), ID: repoInfo.ID}
		}
		sh, err := snapshot.Open(in.Paths.Shadow, r, in.Cfg.Capture.Exclude)
		if err != nil {
			return Result{}, fmt.Errorf("open shadow repository: %w", err)
		}
		// The first snapshot has the HEAD at the start of capture as its
		// parent. It never changes, so it also repairs a base that an older
		// xis moved on resume or compaction.
		if p, err := sh.Git().Run("rev-parse", "--verify", "-q", tr.Session.BaseSnapshot+"^"); err == nil && tr.Session.BaseSnapshot != "" {
			tr.Session.BaseCommit = strings.TrimSpace(p)
		}
		if err := in.deriveEdits(sh, &tr, in.preSessionBase(&tr)); err != nil {
			tr.Warnings = append(tr.Warnings, "edits: "+err.Error())
		}
	}
	dropEmptyHumanEdits(&tr)
	testRuns := in.detectTests(&tr)

	for i := range events {
		events[i].SessionID = tr.Session.ID
		events[i].Payload = in.storedPayload(events[i].Payload)
	}

	err = in.Store.Tx(func(tx *store.Store) error {
		if repo.ID != "" {
			if err := tx.PutRepo(repo); err != nil {
				return err
			}
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
		for _, r := range testRuns {
			if err := tx.PutTestRun(r); err != nil {
				return err
			}
		}
		for _, e := range events {
			if err := tx.PutHookEvent(e); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	for _, w := range tr.Warnings {
		slog.Warn("ingest", "session", tr.Session.ID, "warning", w)
	}
	return Result{SessionID: tr.Session.ID, AgentSessionID: agentSessionID, Steps: len(tr.Steps), Edits: len(tr.Edits), TestRuns: len(testRuns), Warnings: tr.Warnings}, nil
}

func transcriptPaths(events []model.HookEvent) (string, map[string]string) {
	main := ""
	agents := map[string]string{}
	for _, e := range events {
		p, err := claudecode.ParseHookPayload([]byte(e.Payload))
		if err != nil {
			continue
		}
		if p.TranscriptPath != "" {
			main = p.TranscriptPath
		}
		if p.AgentID != "" && p.AgentTranscriptPath != "" {
			agents[p.AgentID] = p.AgentTranscriptPath
		}
	}
	return main, agents
}

func readSubagents(mainPath string, known map[string]string, warnings *[]string) map[string][]claudecode.Entry {
	paths := map[string]string{}
	for id, p := range known {
		paths[id] = p
	}
	dir := filepath.Join(strings.TrimSuffix(mainPath, ".jsonl"), "subagents")
	if files, err := filepath.Glob(filepath.Join(dir, "agent-*.jsonl")); err == nil {
		for _, f := range files {
			id := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "agent-"), ".jsonl")
			if _, ok := paths[id]; !ok {
				paths[id] = f
			}
		}
	}
	out := map[string][]claudecode.Entry{}
	for id, p := range paths {
		entries, warns, err := claudecode.ReadTranscript(p)
		if err != nil {
			continue
		}
		*warnings = append(*warnings, warns...)
		out[id] = entries
	}
	return out
}

func (in *Ingester) redactTrace(tr *model.Trace) {
	max := in.Cfg.Capture.MaxOutputBytes
	for i := range tr.Steps {
		tr.Steps[i].Text = in.redactor.String(tr.Steps[i].Text)
	}
	for i := range tr.ToolCalls {
		tc := &tr.ToolCalls[i]
		tc.Input = redact.Truncate(string(in.redactor.JSON([]byte(tc.Input))), max)
		tc.Output = in.redactor.String(redact.Truncate(tc.Output, max))
	}
	tr.Session.Title = in.redactor.String(tr.Session.Title)
}

// storedPayload keeps hook events small and free of secrets: large tool
// responses are truncated before the event is stored.
// contentFields are the payload fields that can hold code, output or
// secrets. IDs and paths stay as they are, so stored events still join with
// new spool events and with the transcript.
var contentFields = []string{"tool_input", "tool_response", "prompt", "last_assistant_message", "error", "message"}

// storedPayload keeps hook events small and free of secrets: content fields
// are truncated and redacted before the event is stored.
func (in *Ingester) storedPayload(payload string) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		return in.redactor.String(redact.Truncate(payload, in.Cfg.Capture.MaxOutputBytes))
	}
	for _, k := range contentFields {
		v, ok := m[k]
		if !ok {
			continue
		}
		if len(v) > in.Cfg.Capture.MaxOutputBytes {
			v, _ = json.Marshal(redact.Truncate(string(v), in.Cfg.Capture.MaxOutputBytes))
		}
		m[k] = in.redactor.JSON(v)
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func dropEmptyHumanEdits(tr *model.Trace) {
	hasEdits := map[string]bool{}
	for _, e := range tr.Edits {
		hasEdits[e.StepID] = true
	}
	var steps []model.Step
	dropped := map[string]bool{}
	for _, s := range tr.Steps {
		if s.Kind == model.StepHumanEdit && !hasEdits[s.ID] {
			dropped[s.ID] = true
			continue
		}
		s.Seq = len(steps) + 1
		steps = append(steps, s)
	}
	tr.Steps = steps
	for i := range tr.Snapshots {
		if dropped[tr.Snapshots[i].StepID] {
			tr.Snapshots[i].StepID = ""
		}
	}
}

func (in *Ingester) detectTests(tr *model.Trace) []model.TestRun {
	var out []model.TestRun
	for i := range tr.ToolCalls {
		tc := &tr.ToolCalls[i]
		if tc.Category != model.CatShell && tc.Category != model.CatTest {
			continue
		}
		cmd := adapters.Command(tc.Input)
		fw, seg, ok := in.tests.Detect(cmd)
		if !ok {
			continue
		}
		tc.Category = model.CatTest
		sum := testdetect.Outcome(cmd, tc.ExitCode, tc.Output)
		out = append(out, model.TestRun{StepID: tc.StepID, Command: seg, Framework: fw, Passed: sum.Passed, Failed: sum.Failed, Skipped: sum.Skipped, Outcome: sum.Outcome})
	}
	return out
}

func (in *Ingester) IngestAllPending() ([]Result, []error) {
	files, err := capture.ListSpool(in.Paths.Spool)
	if err != nil {
		return nil, []error{err}
	}
	var res []Result
	var errs []error
	for _, f := range files {
		r, err := in.IngestSpool(f.SessionID)
		if err != nil {
			errs = append(errs, fmt.Errorf("session %s: %w", f.SessionID, err))
			continue
		}
		res = append(res, r)
	}
	return res, errs
}
