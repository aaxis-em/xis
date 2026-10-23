package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/aaxis-em/xis/internal/model"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "data", "xis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

var t0 = time.Date(2026, 10, 20, 10, 0, 0, 123456789, time.UTC)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenPermissionsAndMigrations(t *testing.T) {
	s := open(t)
	for _, p := range []string{s.Path(), s.Path() + "-wal"} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v", p, fi.Mode().Perm())
		}
	}
	fi, _ := os.Stat(filepath.Dir(s.Path()))
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %v", fi.Mode().Perm())
	}
	var mode string
	must(t, s.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode))
	if mode != "wal" {
		t.Errorf("journal_mode = %s", mode)
	}
	ms, err := s.Migrations()
	must(t, err)
	if len(ms) != 4 {
		t.Errorf("migrations = %v", ms)
	}
	must(t, s.Check())
	s.Close()
	s2, err := Open(s.Path())
	must(t, err)
	s2.Close()
}

func TestRoundTrip(t *testing.T) {
	s := open(t)
	exit := 1

	repo := model.Repo{ID: "r1", Path: "/a b/ü", RemoteURL: "git@x:y", CreatedAt: t0}
	must(t, s.PutRepo(repo))
	gotRepo, err := s.GetRepo("r1")
	must(t, err)
	if diff := cmp.Diff(repo, gotRepo); diff != "" {
		t.Errorf("repo: %s", diff)
	}

	sess := model.Session{ID: "s1", RepoID: "r1", Agent: "claude-code", AgentVersion: "2.1", AgentSessionID: "uuid",
		Models: []string{"m1", "m2"}, Fidelity: model.FidelityLive, BaseCommit: "abc", BaseSnapshot: "b", FinalSnapshot: "f",
		StartedAt: t0, EndedAt: t0.Add(time.Minute), Cwd: "/w", Title: "t", NoGit: true, InputTokens: 5, OutputTokens: 6, Status: "ended"}
	must(t, s.PutSession(sess))
	gotSess, err := s.GetSession("s1")
	must(t, err)
	if diff := cmp.Diff(sess, gotSess); diff != "" {
		t.Errorf("session: %s", diff)
	}
	found, err := s.FindSession("claude-code", "uuid")
	must(t, err)
	if found.ID != "s1" {
		t.Errorf("find: %v", found.ID)
	}
	list, err := s.ListSessions("r1", 10)
	must(t, err)
	if len(list) != 1 {
		t.Errorf("list sessions: %d", len(list))
	}

	actor := model.Actor{ID: "a1", SessionID: "s1", Kind: "subagent", Name: "x", ParentActorID: "a0"}
	must(t, s.PutActor(actor))
	actors, err := s.ListActors("s1")
	must(t, err)
	if diff := cmp.Diff([]model.Actor{actor}, actors); diff != "" {
		t.Errorf("actors: %s", diff)
	}

	step := model.Step{ID: "st1", SessionID: "s1", ActorID: "a1", ParentStepID: "st0", Seq: 3, TsStart: t0, TsEnd: t0.Add(time.Second),
		Kind: model.StepToolCall, Text: "hello", Snapshot: "c1", Summary: "sum"}
	must(t, s.PutStep(step))
	gotStep, err := s.GetStep("st1")
	must(t, err)
	if diff := cmp.Diff(step, gotStep); diff != "" {
		t.Errorf("step: %s", diff)
	}

	tc := model.ToolCall{StepID: "st1", ToolUseID: "tu", ToolName: "Bash", Category: model.CatTest, Input: "{}", Output: "o", ExitCode: &exit, OK: false, DurationMS: 12}
	must(t, s.PutToolCall(tc))
	gotTC, err := s.GetToolCall("st1")
	must(t, err)
	if diff := cmp.Diff(tc, gotTC); diff != "" {
		t.Errorf("tool call: %s", diff)
	}
	tcs, err := s.ListToolCalls("s1")
	must(t, err)
	if len(tcs) != 1 {
		t.Errorf("tool calls: %d", len(tcs))
	}

	edit := model.Edit{ID: "e1", StepID: "st1", FilePath: "a.go", OldPath: "b.go", BeforeBlob: "x", AfterBlob: "y", Patch: "@@", Added: 2, Removed: 1, RevertedByEditID: "e2"}
	must(t, s.PutEdit(edit))
	edits, err := s.ListEdits("s1")
	must(t, err)
	if diff := cmp.Diff([]model.Edit{edit}, edits); diff != "" {
		t.Errorf("edits: %s", diff)
	}

	tr := model.TestRun{StepID: "st1", Command: "go test", Framework: "go", Passed: 1, Failed: 2, Skipped: 3, Outcome: "fail"}
	must(t, s.PutTestRun(tr))
	trs, err := s.ListTestRuns("s1")
	must(t, err)
	if diff := cmp.Diff([]model.TestRun{tr}, trs); diff != "" {
		t.Errorf("test runs: %s", diff)
	}

	ii := model.IntentItem{ID: "i1", SessionID: "s1", Source: "prompt", SourceStepID: "st1", Text: "fix", Status: "addressed"}
	must(t, s.PutIntentItem(ii))
	iis, err := s.ListIntentItems("s1")
	must(t, err)
	if diff := cmp.Diff([]model.IntentItem{ii}, iis); diff != "" {
		t.Errorf("intent: %s", diff)
	}

	ch := model.Change{ID: "c1", RepoID: "r1", BaseRef: "abc", TargetRef: "worktree", SessionIDs: []string{"s1"}, CreatedAt: t0}
	must(t, s.PutChange(ch))
	gotCh, err := s.GetChange("c1")
	must(t, err)
	if diff := cmp.Diff(ch, gotCh); diff != "" {
		t.Errorf("change: %s", diff)
	}

	unit := model.ReviewUnit{ID: "u1", ChangeID: "c1", Title: "Retry", Ord: 1, Status: "unreviewed", Source: "rule"}
	must(t, s.PutReviewUnit(unit))
	must(t, s.SetReviewUnitStatus("u1", "understood"))
	unit.Title = "Retry 2"
	unit.Status = ""
	must(t, s.PutReviewUnit(unit))
	units, err := s.ListReviewUnits("c1")
	must(t, err)
	if len(units) != 1 || units[0].Status != "understood" || units[0].Title != "Retry 2" {
		t.Errorf("units: %+v", units)
	}

	h := model.Hunk{ID: "h1", ChangeID: "c1", FilePath: "a.go", OldStart: 1, OldLines: 2, NewStart: 3, NewLines: 4, Patch: "@@", ChangeType: model.ChangeBehavior, ReviewUnitID: "u1", ContentHash: "hh"}
	must(t, s.PutHunk(h))
	gotH, err := s.GetHunk("h1")
	must(t, err)
	if diff := cmp.Diff(h, gotH); diff != "" {
		t.Errorf("hunk: %s", diff)
	}

	prov := model.HunkProvenance{HunkID: "h1", StepID: "st1", Lines: 4}
	must(t, s.PutProvenance(prov))
	provs, err := s.ListProvenance("c1")
	must(t, err)
	if diff := cmp.Diff([]model.HunkProvenance{prov}, provs); diff != "" {
		t.Errorf("prov: %s", diff)
	}
	hs, err := s.HunksOfStep("c1", "st1")
	must(t, err)
	if len(hs) != 1 {
		t.Errorf("hunks of step: %v", hs)
	}

	hi := model.HunkIntent{HunkID: "h1", IntentItemID: "i1", Confidence: 0.5}
	must(t, s.PutHunkIntent(hi))
	his, err := s.ListHunkIntent("c1")
	must(t, err)
	if diff := cmp.Diff([]model.HunkIntent{hi}, his); diff != "" {
		t.Errorf("hunk intent: %s", diff)
	}

	rat := model.Rationale{HunkID: "h1", AgentClaim: "c", ClaimStepID: "st1", Explanation: "e [step:st1]", EvidenceStepIDs: []string{"st1"}, Verified: true, Model: "m", CreatedAt: t0}
	must(t, s.PutRationale(rat))
	gotRat, err := s.GetRationale("h1")
	must(t, err)
	if diff := cmp.Diff(rat, gotRat); diff != "" {
		t.Errorf("rationale: %s", diff)
	}

	flag := model.Flag{ID: "f1", ChangeID: "c1", SessionID: "s1", RuleID: "TEST_SKIPPED", Severity: model.SevHigh, Detector: "rule",
		TargetKind: "hunk", TargetID: "h1", Line: 7, Message: "m", EvidenceStepIDs: []string{"st1"}, Fingerprint: "fp1"}
	must(t, s.PutFlag(flag))
	gotFlag, err := s.GetFlag("f1")
	must(t, err)
	if diff := cmp.Diff(flag, gotFlag); diff != "" {
		t.Errorf("flag: %s", diff)
	}
	sup, err := s.SuppressFlag("f1", "known")
	must(t, err)
	if !sup.Suppressed || sup.SuppressReason != "known" {
		t.Errorf("suppress: %+v", sup)
	}
	must(t, s.DeleteFlags("c1", "rule"))
	flag.ID = "f2"
	must(t, s.PutFlag(flag))
	again, err := s.GetFlag("f2")
	must(t, err)
	if !again.Suppressed {
		t.Error("suppression did not survive recompute")
	}

	qa := model.QA{ID: "q1", ChangeID: "c1", Question: "why?", Answer: "because [step:st1]", CitedStepIDs: []string{"st1"}, CitedHunkIDs: []string{"h1"}, Model: "m", CreatedAt: t0}
	must(t, s.PutQA(qa))
	qas, err := s.ListQA("c1")
	must(t, err)
	if diff := cmp.Diff([]model.QA{qa}, qas); diff != "" {
		t.Errorf("qa: %s", diff)
	}

	blob := model.Blob{SHA256: "ab", Size: 3, Content: []byte("abc")}
	must(t, s.PutBlob(blob))
	gotBlob, err := s.GetBlob("ab")
	must(t, err)
	if diff := cmp.Diff(blob, gotBlob); diff != "" {
		t.Errorf("blob: %s", diff)
	}

	ev := model.HookEvent{SessionID: "s1", Key: "k1", Seq: 1, Ts: t0, Event: "PostToolUse", Payload: "{}", Snapshot: "c1"}
	must(t, s.PutHookEvent(ev))
	must(t, s.PutHookEvent(ev))
	evs, err := s.ListHookEvents("s1")
	must(t, err)
	if diff := cmp.Diff([]model.HookEvent{ev}, evs); diff != "" {
		t.Errorf("hook events: %s", diff)
	}

	must(t, s.DeleteSession("s1"))
	if _, err := s.GetSession("s1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted session: %v", err)
	}
	if steps, _ := s.ListSteps("s1"); len(steps) != 0 {
		t.Error("steps left after delete")
	}
}

func TestTxRollback(t *testing.T) {
	s := open(t)
	err := s.Tx(func(tx *Store) error {
		must(t, tx.PutRepo(model.Repo{ID: "r"}))
		return os.ErrInvalid
	})
	if err == nil {
		t.Fatal("want error")
	}
	if _, err := s.GetRepo("r"); !errors.Is(err, ErrNotFound) {
		t.Errorf("repo should not exist: %v", err)
	}
}

func TestStableID(t *testing.T) {
	a := model.StableID(t0, "x", "y")
	b := model.StableID(t0, "x", "y")
	c := model.StableID(t0, "xy")
	if a != b || a == c || len(a) != 26 {
		t.Errorf("ids: %s %s %s", a, b, c)
	}
}
