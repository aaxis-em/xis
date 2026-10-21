package store

import (
	"strings"
	"testing"

	"github.com/aaxis-em/xis/internal/model"
)

func TestAgentTextNearPrefersTheFile(t *testing.T) {
	steps := []model.Step{
		{ID: "u", Kind: model.StepUserMessage, ActorID: "h", Text: "add stats"},
		{ID: "a1", Kind: model.StepAssistantMessage, ActorID: "ag", Text: "I will add a hit counter."},
		{ID: "a2", Kind: model.StepAssistantMessage, ActorID: "ag", Text: "Note: main.go has your uncommitted changes."},
		{ID: "a3", Kind: model.StepAssistantMessage, ActorID: "ag", Text: "store.go needs a hits field; main.go stays."},
		{ID: "e", Kind: model.StepToolCall, ActorID: "ag"},
	}
	d := &SessionData{Steps: steps, StepIndex: map[string]int{},
		Edits: []model.Edit{{StepID: "e", FilePath: "store/store.go"}, {StepID: "e", FilePath: "main.go"}, {StepID: "e", FilePath: "data.go"}}}
	for i, s := range steps {
		d.StepIndex[s.ID] = i
	}
	ids := func(ss []model.Step) (out []string) {
		for _, s := range ss {
			out = append(out, s.ID)
		}
		return out
	}
	for file, want := range map[string]string{"store/store.go": "a1 a3", "main.go": "a1 a2 a3", "data.go": "a1", "": "a1 a2 a3"} {
		if got := ids(d.AgentTextNear("e", file, 4)); strings.Join(got, " ") != want {
			t.Errorf("file %q: got %v, want %s", file, got, want)
		}
	}
	if mentions("see data.go", "a.go") || !mentions("edit (a.go) now", "a.go") {
		t.Error("mentions must match whole file names")
	}
}
