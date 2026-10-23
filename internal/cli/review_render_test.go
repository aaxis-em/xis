package cli

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aaxis-em/xis/internal/attribution"
	"github.com/aaxis-em/xis/internal/config"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/store"
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

func TestE2EHunkStatusPersists(t *testing.T) {
	s := newScenario(t)
	runCalcScenario(s)
	if out, err := run(t, "ingest", "--session", s.session); err != nil {
		t.Fatalf("ingest: %v %s", err, out)
	}
	load := func() (*tui.Data, *store.Store) {
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
		return d, st
	}
	d, st := load()
	m := tui.New(d, tui.Callbacks{SetUnitStatus: st.SetReviewUnitStatus, SetHunkStatus: st.SetHunkStatus}, false)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("U")})

	d2, _ := load()
	first := d2.Units[0]
	for _, h := range first.Hunks {
		if d2.HunkStatus[tui.HunkKey(h)] != "understood" {
			t.Errorf("hunk %s of %q not understood after reopening", h.FilePath, first.Title)
		}
	}
	if first.Status != "understood" {
		t.Errorf("unit status = %q", first.Status)
	}
}

func TestE2ELiveReviewSeesNewPrompts(t *testing.T) {
	s := newScenario(t)
	s.hook("SessionStart", map[string]any{"source": "startup"})
	s.prompt("Fix Add.")
	s.tool("t1-edit", "Edit", map[string]any{"file_path": filepath.Join(s.repo, "calc.go")}, func() { s.write("calc.go", calcFixed) }, "", 0)
	s.hook("Stop", map[string]any{"last_assistant_message": "Done."})
	if out, err := run(t, "ingest", "--session", s.session); err != nil {
		t.Fatalf("ingest: %v %s", err, out)
	}
	st := openTestStore(t)
	app := &App{Paths: config.DefaultPaths(), Cfg: config.Default(), RepoDir: s.repo}
	opts := attribution.Options{Mode: attribution.ModeSession}
	ca, err := app.analyzeChange(st, opts)
	if err != nil {
		t.Fatal(err)
	}
	live := newLiveReview(app, st, opts, ca, false)
	if d, err := live.refresh(); d != nil || err != nil {
		t.Fatalf("refresh without new events: %v %v", d != nil, err)
	}

	s.prompt("Now add Sub.")
	s.tool("t2-edit", "Edit", map[string]any{"file_path": filepath.Join(s.repo, "sub.go")}, func() {
		s.write("sub.go", "package calc\n\nfunc Sub(a, b int) int { return a - b }\n")
	}, "", 0)
	d, err := live.refresh()
	if err != nil || d == nil {
		t.Fatalf("refresh after a new prompt: %v %v", d != nil, err)
	}
	prompts, files := 0, map[string]bool{}
	for _, sd := range d.Sessions {
		for _, st := range sd.Steps {
			if st.Kind == model.StepUserMessage {
				prompts++
			}
		}
	}
	for _, u := range d.Units {
		for _, h := range u.Hunks {
			files[h.FilePath] = true
		}
	}
	if prompts != 2 || !files["sub.go"] {
		t.Errorf("after the refresh: %d prompts, files %v", prompts, files)
	}
}
