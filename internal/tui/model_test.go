package tui

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/store"
)

func testData(units, hunksPerUnit int) *Data {
	sd := &store.SessionData{
		Session:   model.Session{ID: "s1", Agent: "claude-code", Fidelity: model.FidelityLive},
		Actors:    map[string]model.Actor{"a": {ID: "a", Kind: "agent"}, "h": {ID: "h", Kind: "human"}},
		ToolCalls: map[string]model.ToolCall{},
		StepIndex: map[string]int{},
	}
	addStep := func(kind model.StepKind, actor, text string) string {
		id := fmt.Sprintf("step%03d", len(sd.Steps)+1)
		sd.StepIndex[id] = len(sd.Steps)
		sd.Steps = append(sd.Steps, model.Step{ID: id, Seq: len(sd.Steps) + 1, Kind: kind, ActorID: actor, Text: text})
		return id
	}
	addStep(model.StepUserMessage, "h", "Fix the login timeout")
	read := addStep(model.StepToolCall, "a", "Read src/auth/session.go")
	sd.ToolCalls[read] = model.ToolCall{StepID: read, ToolName: "Read", Category: model.CatRead}
	addStep(model.StepAssistantMessage, "a", "Wrap refresh in a retry so the login survives a slow server.")
	edit := addStep(model.StepToolCall, "a", "Edit src/auth/session.go")
	sd.ToolCalls[edit] = model.ToolCall{StepID: edit, ToolName: "Edit", Category: model.CatEdit}
	d := &Data{
		Title: "fix-login-timeout", Agent: "claude-code", Fidelity: "live", Steps: len(sd.Steps),
		Sessions: map[string]*store.SessionData{"s1": sd}, Provenance: map[string][]model.HunkProvenance{},
		Rationales: map[string]model.Rationale{}, StepSession: map[string]string{}, ActorKind: map[string]string{edit: "agent"},
	}
	for _, s := range sd.Steps {
		d.StepSession[s.ID] = "s1"
		d.Timeline = append(d.Timeline, TimelineStep{Step: s})
	}
	for u := 0; u < units; u++ {
		unit := Unit{ReviewUnit: model.ReviewUnit{ID: fmt.Sprintf("u%d", u), Title: fmt.Sprintf("Unit %d", u+1), Ord: u + 1, Status: "unreviewed"}}
		for h := 0; h < hunksPerUnit; h++ {
			id := fmt.Sprintf("h%d-%d", u, h)
			hk := model.Hunk{ID: id, FilePath: fmt.Sprintf("src/auth/file%d.go", u), NewStart: 40 + h*10, OldStart: 40 + h*10, ChangeType: model.ChangeBehavior,
				Patch: fmt.Sprintf("@@ -%d,2 +%d,3 @@\n func Refresh() {\n+\tfor i := 0; i < cfg.Retries; i++ {\n-\treturn c.do(req)\n+\t\terr = c.do(req) // marker-%s\n", 40+h*10, 40+h*10, id)}
			unit.Hunks = append(unit.Hunks, hk)
			d.Provenance[id] = []model.HunkProvenance{{HunkID: id, StepID: edit, Lines: 3}}
		}
		d.Units = append(d.Units, unit)
	}
	d.Files = units
	d.Flags = []model.Flag{{ID: "f1", RuleID: "TEST_EDIT_AFTER_FAILURE", Severity: model.SevHigh, TargetKind: "hunk", TargetID: fmt.Sprintf("h%d-0", units-1), Message: "edited a test after failure"}}
	return d
}

func key(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func waitFor(t *testing.T, tm *teatest.TestModel, want ...string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		for _, w := range want {
			if !bytes.Contains(b, []byte(w)) {
				return false
			}
		}
		return true
	}, teatest.WithDuration(3*time.Second))
}

func TestNavigation(t *testing.T) {
	var saved []string
	m := New(testData(4, 3), Callbacks{SetUnitStatus: func(id, st string) error { saved = append(saved, id+"="+st); return nil }}, false)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(150, 40))
	waitFor(t, tm, "UNITS", "WHY", "src/auth/file0.go", "Agent text near this edit")
	for _, k := range []string{"j", "j", "u", "]", "tab", "j", "x", "G", "[", "g"} {
		tm.Send(key(k))
	}
	tm.Send(key("q"))
	fm := tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second)).(*Model)
	unit, hunk, focus, _ := fm.Selection()
	if unit != 2 || hunk != 0 || focus != int(paneDiff) {
		t.Errorf("selection = unit %d hunk %d focus %d", unit, hunk, focus)
	}
	want := []string{"u2=understood", "u3=questioned"}
	if strings.Join(saved, ",") != strings.Join(want, ",") {
		t.Errorf("saved = %v", saved)
	}
}

func TestLayouts(t *testing.T) {
	cases := []struct {
		w       int
		want    []string
		notWant string
	}{
		{150, []string{"UNITS", "DIFF", "WHY"}, ""},
		{110, []string{"UNITS", "src/auth/file0.go"}, "WHY"},
		{80, []string{"UNITS"}, "file0.go  [behavior]"},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.w), func(t *testing.T) {
			m := New(testData(2, 2), Callbacks{}, false)
			m.width, m.height = c.w, 30
			v := m.View()
			for _, w := range c.want {
				if !strings.Contains(v, w) && (w != "DIFF" || !strings.Contains(v, "[behavior]")) {
					t.Errorf("width %d: missing %q", c.w, w)
				}
			}
			if c.notWant != "" && strings.Contains(v, c.notWant) {
				t.Errorf("width %d: unexpected %q", c.w, c.notWant)
			}
			for i, line := range strings.Split(v, "\n") {
				if w := len([]rune(stripANSI(line))); w > c.w {
					t.Errorf("width %d: line %d is %d wide", c.w, i, w)
				}
			}
		})
	}
	m := New(testData(2, 2), Callbacks{}, false)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 30))
	tm.Send(key("tab"))
	tm.Send(key("tab"))
	waitFor(t, tm, "WHY", "Instruction")
	tm.Send(key("q"))
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestViewsAndFlags(t *testing.T) {
	m := New(testData(3, 2), Callbacks{}, false)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(150, 40))
	tm.Send(key("n"))
	tm.Send(key("t"))
	waitFor(t, tm, "TIMELINE", "Fix the login timeout")
	tm.Send(key("enter"))
	waitFor(t, tm, "Step 1 · user_message")
	tm.Send(key("esc"))
	tm.Send(key("esc"))
	tm.Send(key("f"))
	waitFor(t, tm, "FLAGS (1)", "TEST_EDIT_AFTER_FAILURE")
	tm.Send(key("enter"))
	tm.Send(key("a"))
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("why retry?")})
	tm.Send(key("enter"))
	waitFor(t, tm, "LLM analysis off")
	tm.Send(key("esc"))
	tm.Send(key("/"))
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("marker-h1-1")})
	tm.Send(key("enter"))
	tm.Send(key("q"))
	fm := tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second)).(*Model)
	unit, hunk, _, _ := fm.Selection()
	if unit != 1 || hunk != 1 {
		t.Errorf("search: unit %d hunk %d", unit, hunk)
	}
}

func TestLargeChangeIsFast(t *testing.T) {
	m := New(testData(200, 10), Callbacks{}, true)
	m.width, m.height = 160, 50
	start := time.Now()
	for i := 0; i < 300; i++ {
		m.Update(key("j"))
		if i%10 == 0 {
			m.Update(key("]"))
		}
		_ = m.View()
	}
	m.focus = paneDiff
	for i := 0; i < 300; i++ {
		m.Update(key("j"))
		_ = m.View()
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("600 key presses on 2,000 hunks took %s", d)
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			in = true
		case in && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestKeymapOverride(t *testing.T) {
	m := New(testData(3, 1), Callbacks{}, false)
	if err := m.SetKeymap(map[string]string{"next_unit": "ctrl+n", "quit": "Q"}); err != nil {
		t.Fatal(err)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if u, _, _, _ := m.Selection(); u != 1 {
		t.Errorf("ctrl+n did not move to the next unit: %d", u)
	}
	m.Update(key("]"))
	if u, _, _, _ := m.Selection(); u != 2 {
		t.Errorf("default key stopped working: %d", u)
	}
	if err := m.SetKeymap(map[string]string{"fly": "z"}); err == nil {
		t.Error("unknown action accepted")
	}
}
