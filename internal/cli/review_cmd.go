package cli

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/store"
	"github.com/aaxis-em/xis/internal/termbg"
	"github.com/aaxis-em/xis/internal/tui"
)

func init() { register(newReviewCmd) }

func buildReviewData(st *store.Store, ca *changeAnalysis, llmOn bool) (*tui.Data, error) {
	res := ca.Result
	d := &tui.Data{
		Flags: ca.Flags, Sessions: map[string]*store.SessionData{}, Provenance: map[string][]model.HunkProvenance{},
		Rationales: map[string]model.Rationale{}, StepSession: res.StepSession, ActorKind: res.ActorKind,
		LLMOn: llmOn, RepoTop: ca.Engine.Repo.Top, LineSteps: res.LineSteps,
	}
	for _, p := range res.Provenance {
		d.Provenance[p.HunkID] = append(d.Provenance[p.HunkID], p)
	}
	files := map[string]bool{}
	for _, h := range res.Hunks {
		files[h.FilePath] = true
	}
	d.Files = len(files)
	for _, sd := range ca.Sessions {
		d.Sessions[sd.Session.ID] = sd
		d.Steps += len(sd.Steps)
		d.Intent = append(d.Intent, sd.Intent...)
	}
	if len(ca.Sessions) > 0 {
		s := ca.Sessions[len(ca.Sessions)-1].Session
		d.Title, d.Agent, d.Fidelity, d.Status = s.Title, s.Agent, string(s.Fidelity), s.Status
		if d.Title == "" {
			d.Title = shortID(s.ID)
		}
		for _, sd := range ca.Sessions {
			if sd.Session.Fidelity != s.Fidelity {
				d.Fidelity = "mixed"
			}
		}
		tl := buildTimeline(st, ca.Sessions[len(ca.Sessions)-1], res)
		for _, t := range tl.Steps {
			step, _ := ca.Sessions[len(ca.Sessions)-1].Step(t.ID)
			d.Timeline = append(d.Timeline, tui.TimelineStep{Step: step, Depth: t.Depth, Actor: t.Actor, Tool: t.Tool, Files: t.Files, Test: t.Test, Survive: t.Survive, Abandoned: t.Abandoned})
		}
	}
	units, err := st.ListReviewUnits(res.Change.ID)
	if err != nil {
		return nil, err
	}
	hunks, err := st.ListHunks(res.Change.ID)
	if err != nil {
		return nil, err
	}
	byUnit := map[string][]model.Hunk{}
	for _, h := range hunks {
		byUnit[h.ReviewUnitID] = append(byUnit[h.ReviewUnitID], h)
	}
	for _, u := range units {
		d.Units = append(d.Units, tui.Unit{ReviewUnit: u, Hunks: byUnit[u.ID]})
	}
	rats, err := st.ListRationales(res.Change.ID)
	if err != nil {
		return nil, err
	}
	for _, r := range rats {
		d.Rationales[r.HunkID] = r
	}
	return d, nil
}

func newReviewCmd() *cobra.Command {
	var cf changeFlags
	cmd := &cobra.Command{
		Use:   "review [SESSION]",
		Short: "Review a change hunk by hunk in the terminal UI",
		Long: `Open the review UI. It shows the review units, the diff of each hunk and the WHY pane:
the steps that wrote the hunk, the agent text near the edit, the instruction and the evidence.
The UI works without an LLM.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := newApp(cmd)
			session := ""
			if len(args) == 1 {
				session = args[0]
			}
			opts, err := cf.options(session)
			if err != nil {
				return err
			}
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			ca, err := a.analyzeChange(st, opts)
			if err != nil {
				return err
			}
			llmOn := a.Cfg.LLM.EffectiveProvider() != "none"
			data, err := buildReviewData(st, ca, llmOn)
			if err != nil {
				return err
			}
			cb := tui.Callbacks{SetUnitStatus: st.SetReviewUnitStatus}
			if llmOn {
				cb.Ask = a.askFunc(st, ca)
			}
			if !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsCygwinTerminal(os.Stdout.Fd()) {
				return userErr("xis review needs a terminal", "Use `xis report --md` or `xis flags` for non-interactive output.", nil)
			}
			theme := a.Cfg.UI.Theme
			m := tui.New(data, cb, !a.NoColor && theme != "none")
			if theme == "light" || theme == "auto" && !termbg.Dark() {
				m.SetLightTheme()
			}
			if err := m.SetKeymap(a.Cfg.UI.Keymap); err != nil {
				return userErr(err.Error(), "Fix ui.keymap in the config.", err)
			}
			_, err = tea.NewProgram(m, tea.WithAltScreen()).Run()
			return err
		},
	}
	cf.add(cmd)
	return cmd
}
