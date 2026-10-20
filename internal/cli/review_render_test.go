package cli

import (
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaxis-em/xis/internal/attribution"
	"github.com/aaxis-em/xis/internal/config"
	"github.com/aaxis-em/xis/internal/tui"
)

func TestE2EReviewData(t *testing.T) {
	s := newScenario(t)
	runCalcScenario(s)
	st := openTestStore(t)
	app := &App{Paths: config.DefaultPaths(), Cfg: config.Default(), RepoDir: s.repo}
	ca, err := app.analyzeChange(st, attribution.Options{Mode: attribution.ModeSession})
	if err != nil {
		t.Fatal(err)
	}
	d, err := buildReviewData(st, ca, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Units) == 0 || len(d.Timeline) == 0 || len(d.Flags) != 2 {
		t.Fatalf("units=%d timeline=%d flags=%d", len(d.Units), len(d.Timeline), len(d.Flags))
	}
	n := 0
	for _, u := range d.Units {
		n += len(u.Hunks)
	}
	if n != len(ca.Result.Hunks) {
		t.Errorf("units hold %d hunks, change has %d", n, len(ca.Result.Hunks))
	}
	m := tui.New(d, tui.Callbacks{SetUnitStatus: st.SetReviewUnitStatus}, false)
	m.Update(tea.WindowSizeMsg{Width: 150, Height: 30})
	if os.Getenv("XIS_SHOW_TUI") != "" {
		t.Log("\n" + m.View())
	}
}
