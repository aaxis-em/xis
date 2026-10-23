package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaxis-em/xis/internal/model"
)

func TestListRowsKeepTheSelection(t *testing.T) {
	m := New(testData(1, 1), Callbacks{}, false)
	for n := 1; n <= 12; n++ {
		for size := 1; size <= 8; size++ {
			for sel := 0; sel < n; sel++ {
				rows := m.listRows(n, sel, size, func(i int) string { return fmt.Sprintf("item %d", i) })
				if len(rows) != min(n, size) {
					t.Fatalf("n=%d size=%d sel=%d: %d rows", n, size, sel, len(rows))
				}
				if !contains(rows, fmt.Sprintf("item %d", sel)) {
					t.Fatalf("n=%d size=%d sel=%d: selection hidden in %q", n, size, sel, rows)
				}
			}
		}
	}
}

func contains(rows []string, s string) bool {
	for _, r := range rows {
		if r == s {
			return true
		}
	}
	return false
}

func TestLongListsScroll(t *testing.T) {
	d := testData(40, 1)
	sd := d.Sessions["s1"]
	for i := 2; i <= 25; i++ {
		id := fmt.Sprintf("p%03d", i)
		sd.StepIndex[id] = len(sd.Steps)
		sd.Steps = append(sd.Steps, model.Step{ID: id, Seq: len(sd.Steps) + 1, Kind: model.StepUserMessage, ActorID: "h", Text: fmt.Sprintf("prompt number %d", i)})
	}
	m := New(d, Callbacks{}, false)
	m.Update(tea.WindowSizeMsg{Width: 150, Height: 30})
	lines := func() []string { return strings.Split(stripANSI(m.View()), "\n") }

	m.Update(key("G"))
	view := strings.Join(lines(), "\n")
	if !strings.Contains(view, "▸ 40 Unit 40") || !strings.Contains(view, "↑") {
		t.Errorf("last unit not shown:\n%s", view)
	}
	m.Update(key("p"))
	view = strings.Join(lines(), "\n")
	if !strings.Contains(view, "▸ #1 Fix the login") || !strings.Contains(view, "↓") {
		t.Errorf("first prompt or the hidden prompts below it not shown:\n%s", view)
	}
	for i := 1; i < 20; i++ {
		m.Update(key("p"))
	}
	view = strings.Join(lines(), "\n")
	if !strings.Contains(view, "▸ #23 prompt number 20") || !strings.Contains(view, "↑") {
		t.Errorf("prompt 20 or the hidden prompts above it not shown:\n%s", view)
	}
	if n := len(lines()); n != 30 {
		t.Errorf("view has %d lines, want 30", n)
	}
}
