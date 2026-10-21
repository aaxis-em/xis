package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/aaxis-em/xis/internal/model"
)

func twoPromptData() *Data {
	d := testData(3, 2)
	sd := d.Sessions["s1"]
	first := "step004"
	add := func(kind model.StepKind, actor, text string) string {
		id := fmt.Sprintf("step%03d", len(sd.Steps)+1)
		sd.StepIndex[id] = len(sd.Steps)
		sd.Steps = append(sd.Steps, model.Step{ID: id, Seq: len(sd.Steps) + 1, Kind: kind, ActorID: actor, Text: text})
		d.StepSession[id] = "s1"
		return id
	}
	add(model.StepUserMessage, "h", "Also log each retry")
	second := add(model.StepToolCall, "a", "Edit src/auth/file1.go")
	d.ActorKind[second] = "agent"
	d.Provenance["h1-0"] = []model.HunkProvenance{{HunkID: "h1-0", StepID: first, Lines: 1}, {HunkID: "h1-0", StepID: second, Lines: 1}}
	d.Provenance["h1-1"] = []model.HunkProvenance{{HunkID: "h1-1", StepID: second, Lines: 3}}
	d.LineSteps = map[string]map[int]string{"src/auth/file1.go": {41: first, 42: second}}
	return d
}

func unitHunks(m *Model) []int {
	var out []int
	for _, u := range m.units {
		out = append(out, len(u.Hunks))
	}
	return out
}

func TestPromptFilter(t *testing.T) {
	m := New(twoPromptData(), Callbacks{}, false)
	m.width, m.height = 160, 40
	if len(m.turns) != 2 || m.prompt != -1 || len(m.units) != 3 {
		t.Fatalf("start: %d turns, prompt %d, %d units", len(m.turns), m.prompt, len(m.units))
	}
	view := stripANSI(m.View())
	for _, want := range []string{"PROMPTS (p/P)", "All prompts", "#1 Fix the login timeout", "#5 Also log each retry"} {
		if !strings.Contains(view, want) {
			t.Errorf("view lacks %q", want)
		}
	}

	m.Update(key("p"))
	if got := unitHunks(m); m.prompt != 0 || len(got) != 3 || got[1] != 1 {
		t.Errorf("prompt 1: prompt %d, hunks per unit %v", m.prompt, got)
	}
	m.Update(key("p"))
	if got := unitHunks(m); m.prompt != 1 || len(got) != 1 || got[0] != 2 {
		t.Errorf("prompt 2: prompt %d, hunks per unit %v", m.prompt, got)
	}
	if !strings.Contains(stripANSI(m.View()), "UNITS · prompt #5") {
		t.Error("units title does not name the prompt")
	}
	m.Update(key("p"))
	if m.prompt != -1 || len(m.units) != 3 {
		t.Errorf("third p: prompt %d, %d units", m.prompt, len(m.units))
	}
	m.Update(key("P"))
	if m.prompt != 1 {
		t.Errorf("P from all: prompt %d", m.prompt)
	}

	m.Update(key("n"))
	if h, _ := m.curHunk(); m.prompt != -1 || h.ID != "h2-0" {
		t.Errorf("flag jump: prompt %d, hunk %s", m.prompt, h.ID)
	}
}

func TestPromptFilterDimsOtherLines(t *testing.T) {
	m := New(twoPromptData(), Callbacks{}, true)
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	m.st = newStyles(true)
	m.selectPrompt(1)
	h, _ := m.curHunk()
	if h.ID != "h1-0" {
		t.Fatalf("hunk %s", h.ID)
	}
	body := m.hl.render(h, m.st)
	got := m.dimOtherPrompts(h, body)
	dim := m.st.dim.Render("+    for i := 0; i < cfg.Retries; i++ {")
	if got[1] != dim {
		t.Errorf("line of the other prompt not dimmed: %q", got[1])
	}
	if got[3] != body[3] {
		t.Errorf("line of the selected prompt changed: %q", got[3])
	}
}
