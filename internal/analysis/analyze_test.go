package analysis

import (
	"context"
	"strings"
	"testing"

	"github.com/aaxis-em/xis/internal/llm"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/store"
)

func fixture() Input {
	d := &store.SessionData{
		Session:   model.Session{ID: "S"},
		Actors:    map[string]model.Actor{"a": {ID: "a", Kind: "agent"}, "h": {ID: "h", Kind: "human"}},
		ToolCalls: map[string]model.ToolCall{},
		StepIndex: map[string]int{},
	}
	add := func(id string, kind model.StepKind, actor, text string) {
		d.StepIndex[id] = len(d.Steps)
		d.Steps = append(d.Steps, model.Step{ID: id, SessionID: "S", Seq: len(d.Steps) + 1, Kind: kind, ActorID: actor, Text: text})
	}
	add("P", model.StepUserMessage, "h", "Fix calc.go")
	add("A", model.StepAssistantMessage, "a", "I will edit calc.go so the tests pass.")
	add("E", model.StepToolCall, "a", "Edit calc.go")
	add("T", model.StepToolCall, "a", "go test")
	d.ToolCalls["T"] = model.ToolCall{StepID: "T", ToolName: "Bash", Category: model.CatTest}
	d.TestRuns = []model.TestRun{{StepID: "T", Outcome: model.OutcomePass}}
	d.Edits = []model.Edit{{StepID: "E", FilePath: "calc.go"}}
	add("D", model.StepAssistantMessage, "a", "Edited calc.go and all tests pass.")
	return Input{
		Change:     model.Change{ID: "C"},
		Hunks:      []model.Hunk{{ID: "H1", FilePath: "calc.go", Patch: "@@ -1 +1 @@\n+x"}, {ID: "H2", FilePath: "b.go", Patch: "@@ -1 +1 @@\n+y"}},
		Provenance: map[string][]model.HunkProvenance{"H1": {{HunkID: "H1", StepID: "E", Lines: 1}}, "H2": {{HunkID: "H2", StepID: "E", Lines: 1}}},
		Sessions:   []*store.SessionData{d},
	}
}

func run(t *testing.T, respond func(llm.Request) (string, error)) (*Output, *llm.Mock) {
	t.Helper()
	m := llm.NewMock()
	m.Respond = respond
	a := &Analyzer{LLM: &llm.Client{Provider: m}, SendCode: true}
	out, err := a.Run(context.Background(), fixture())
	if err != nil {
		t.Fatal(err)
	}
	return out, m
}

func TestGroupingFallback(t *testing.T) {
	for name, units := range map[string]string{
		"duplicate": `{"units":[{"title":"A","hunks":["h1","h2"]},{"title":"B","hunks":["h2"]}]}`,
		"missing":   `{"units":[{"title":"A","hunks":["h1"]}]}`,
		"unknown":   `{"units":[{"title":"A","hunks":["h1","h2","h7"]}]}`,
		"no title":  `{"units":[{"title":"","hunks":["h1","h2"]}]}`,
	} {
		out, _ := run(t, func(r llm.Request) (string, error) {
			if r.Task == "group" {
				return units, nil
			}
			return "{}", nil
		})
		if !out.GroupingFallback || len(out.Units) != 0 {
			t.Errorf("%s: fallback=%v units=%d", name, out.GroupingFallback, len(out.Units))
		}
	}
	out, _ := run(t, func(r llm.Request) (string, error) {
		if r.Task == "group" {
			return `{"units":[{"title":"Second","hunks":["h2"]},{"title":"First","hunks":["h1"]}]}`, nil
		}
		return "{}", nil
	})
	if out.GroupingFallback || len(out.Units) != 2 || out.HunkUnit["H1"] != out.Units[1].ID {
		t.Errorf("valid grouping rejected: %+v", out)
	}
}

func TestSendCodeFalse(t *testing.T) {
	m := llm.NewMock()
	a := &Analyzer{LLM: &llm.Client{Provider: m}, SendCode: false}
	in := fixture()
	in.Hunks[0].Patch = "@@ -1 +1 @@\n+SECRET_CODE_LINE"
	if _, err := a.Run(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	for _, r := range m.All() {
		if strings.Contains(r.Messages[0].Content, "SECRET_CODE_LINE") {
			t.Errorf("%s request contains file content with send_code = false", r.Task)
		}
	}
}

func TestVerify(t *testing.T) {
	in := fixture()
	d := in.Sessions[0]
	final, _ := d.Step("D")
	early, _ := d.Step("A")
	cases := []struct {
		claim string
		step  model.Step
		want  bool
	}{
		{"Edited calc.go and all tests pass.", final, true},
		{"All tests pass.", early, false},
		{"Edited other.go.", final, false},
		{"I made it cleaner.", final, false},
		{"The tests fail now.", final, false},
	}
	for _, c := range cases {
		if got := Verify(c.claim, c.step, d); got != c.want {
			t.Errorf("Verify(%q) = %v, want %v", c.claim, got, c.want)
		}
	}
}
