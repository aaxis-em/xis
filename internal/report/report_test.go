package report

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/aaxis-em/xis/internal/gitx"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/store"
)

var update = flag.Bool("update", false, "rewrite golden files")

var t0 = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func input(flags int) Input {
	d := &store.SessionData{
		Session:   model.Session{ID: "01M5CTQV4E0000000000000000", Agent: "claude-code", AgentVersion: "2.1.304", Models: []string{"claude-sonnet-5-5"}, Fidelity: model.FidelityLive, StartedAt: t0, EndedAt: t0.Add(3*time.Minute + 12*time.Second), Status: "ended"},
		Actors:    map[string]model.Actor{"a": {ID: "a", Kind: "agent"}, "h": {ID: "h", Kind: "human"}},
		ToolCalls: map[string]model.ToolCall{},
		StepIndex: map[string]int{},
		Intent:    []model.IntentItem{{Text: "Fix the Add function", Status: "addressed"}, {Text: "Add a test for Sub", Status: "unaddressed"}},
	}
	for i, k := range []model.StepKind{model.StepUserMessage, model.StepToolCall, model.StepToolCall, model.StepToolCall, model.StepHumanEdit} {
		id := fmt.Sprintf("st%d", i+1)
		actor := "a"
		if k != model.StepToolCall {
			actor = "h"
		}
		d.StepIndex[id] = i
		d.Steps = append(d.Steps, model.Step{ID: id, Seq: i + 1, Kind: k, ActorID: actor})
	}
	d.TestRuns = []model.TestRun{{StepID: "st3", Command: "go test ./...", Outcome: "fail"}, {StepID: "st4", Command: "go test ./...", Outcome: "pass"}}
	in := Input{
		Change: model.Change{ID: "C1", BaseRef: "3f786850e387550fdab836ed7e6dc881de23001b", TargetRef: "worktree"},
		Hunks: []model.Hunk{
			{ID: "h1", FilePath: "calc.go", ReviewUnitID: "u1"},
			{ID: "h2", FilePath: "calc_test.go", ReviewUnitID: "u2"},
			{ID: "h3", FilePath: "README.md", ReviewUnitID: "u3"},
		},
		Files:      []gitx.FilePatch{{Path: "calc.go", Added: 2, Removed: 1}, {Path: "calc_test.go", Added: 3}, {Path: "README.md", Added: 2}},
		Provenance: map[string][]model.HunkProvenance{"h1": {{StepID: "st2", Lines: 3}}, "h2": {{StepID: "st3", Lines: 3}}, "h3": {{StepID: "st5", Lines: 2}}},
		ActorKind:  map[string]string{"st2": "agent", "st3": "agent", "st5": "human"},
		Sessions:   []*store.SessionData{d},
		Units: []model.ReviewUnit{
			{ID: "u1", Title: "Fix addition", Status: "understood", Source: "llm"},
			{ID: "u2", Title: "Tests: calc_test.go", Status: "unreviewed", Source: "rule"},
			{ID: "u3", Title: "Docs: README.md", Status: "questioned", Source: "rule"},
		},
		Rationales: map[string]model.Rationale{"h1": {Explanation: "Add now adds instead of subtracting [hunk:h1].", AgentClaim: "The bug is in Add: it subtracts.", Verified: false}},
		Flags: []model.Flag{
			{ID: "f1", Severity: "high", RuleID: "TEST_EDIT_AFTER_FAILURE", TargetKind: "step", TargetID: "st3", Message: "The agent edited calc_test.go after a failing test run."},
			{ID: "f2", Severity: "medium", RuleID: "TEST_FILE_MODIFIED", TargetKind: "file", TargetID: "calc_test.go", Message: "The agent changed the test file calc_test.go.", Suppressed: true, SuppressReason: "the old test was wrong"},
		},
		XisVersion: "v0.1.0", NoteRef: "refs/notes/xis", GeneratedAt: t0,
	}
	for i := 0; i < flags; i++ {
		in.Flags = append(in.Flags, model.Flag{ID: fmt.Sprintf("x%d", i), Severity: "low", RuleID: "LINT_SUPPRESSED", TargetKind: "file", TargetID: fmt.Sprintf("f%d.go", i), Message: "A lint suppression was added."})
	}
	return in
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		_ = os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if diff := cmp.Diff(string(want), got); diff != "" {
		t.Errorf("%s (-want +got):\n%s", name, diff)
	}
}

func TestMarkdownGolden(t *testing.T) {
	r := Build(input(0))
	golden(t, "report.golden.md", Markdown(r, false))
	b, _ := json.MarshalIndent(r, "", "  ")
	golden(t, "report.golden.json", string(b)+"\n")
}

func TestMarkdownLength(t *testing.T) {
	r := Build(input(400))
	short := Markdown(r, false)
	if n := strings.Count(short, "\n"); n > 160 {
		t.Errorf("default report has %d lines", n)
	}
	if !strings.Contains(short, "Run `xis report --full`") {
		t.Error("no hint about --full")
	}
	if n := strings.Count(Markdown(r, true), "LINT_SUPPRESSED"); n != 400 {
		t.Errorf("full report lists %d of 400 flags", n)
	}
}
