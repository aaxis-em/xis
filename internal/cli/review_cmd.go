package cli

import (
	"fmt"
	"os"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/aaxis-em/xis/internal/attribution"
	"github.com/aaxis-em/xis/internal/capture"
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
	if d.HunkStatus, err = loadHunkStatus(st, d.Units); err != nil {
		return nil, err
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
			live := newLiveReview(a, st, opts, ca, llmOn)
			cb := tui.Callbacks{SetUnitStatus: st.SetReviewUnitStatus, SetHunkStatus: st.SetHunkStatus, Refresh: live.refresh}
			if llmOn {
				cb.Ask = func(q string) (string, error) { return a.askFunc(live.analysis())(q) }
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

// loadHunkStatus reads the review status of each hunk. A unit that an older
// xis marked as a whole gives its status to its hunks that have none.
func loadHunkStatus(st *store.Store, units []tui.Unit) (map[string]string, error) {
	var keys []string
	for _, u := range units {
		for _, h := range u.Hunks {
			keys = append(keys, tui.HunkKey(h))
		}
	}
	status, err := st.HunkStatuses(keys)
	if err != nil {
		return nil, err
	}
	for _, u := range units {
		if u.Status != "understood" && u.Status != "questioned" {
			continue
		}
		marked := false
		for _, h := range u.Hunks {
			marked = marked || status[tui.HunkKey(h)] != ""
		}
		if marked {
			continue
		}
		for _, h := range u.Hunks {
			if err := st.SetHunkStatus(tui.HunkKey(h), u.Status); err != nil {
				return nil, err
			}
			status[tui.HunkKey(h)] = u.Status
		}
	}
	return status, nil
}

// liveReview keeps an open review up to date. When a session of the change
// gets new events, it analyzes the change again, which ingests them first.
type liveReview struct {
	a     *App
	st    *store.Store
	opts  attribution.Options
	llmOn bool
	spool map[string]string
	ended map[string]time.Time

	mu sync.Mutex
	ca *changeAnalysis
}

func newLiveReview(a *App, st *store.Store, opts attribution.Options, ca *changeAnalysis, llmOn bool) *liveReview {
	if (opts.Mode == "" || opts.Mode == attribution.ModeSession) && len(ca.Sessions) > 0 {
		opts.SessionID = ca.Sessions[0].Session.ID
	}
	l := &liveReview{a: a, st: st, opts: opts, llmOn: llmOn, ca: ca, spool: map[string]string{}, ended: map[string]time.Time{}}
	for _, sd := range ca.Sessions {
		l.ended[sd.Session.ID] = sd.Session.EndedAt
	}
	return l
}

func (l *liveReview) analysis() *changeAnalysis {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ca
}

func (l *liveReview) refresh() (*tui.Data, error) {
	changed := false
	for _, sd := range l.analysis().Sessions {
		s := sd.Session
		now, err := l.st.GetSession(s.ID)
		if err != nil {
			return nil, err
		}
		changed = changed || l.spoolChanged(s.AgentSessionID) || !now.EndedAt.Equal(l.ended[s.ID])
	}
	if !changed {
		return nil, nil
	}
	ca, err := l.a.analyzeChange(l.st, l.opts)
	if err != nil {
		return nil, err
	}
	for _, sd := range ca.Sessions {
		l.ended[sd.Session.ID] = sd.Session.EndedAt
	}
	data, err := buildReviewData(l.st, ca, l.llmOn)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	l.ca = ca
	l.mu.Unlock()
	return data, nil
}

// spoolChanged reports new events that the hooks wrote since the last check.
func (l *liveReview) spoolChanged(agentSessionID string) bool {
	fi, err := os.Stat(capture.SpoolPath(l.a.Paths.Spool, agentSessionID))
	if err != nil {
		return false
	}
	sig := fmt.Sprint(fi.Size(), fi.ModTime().UnixNano())
	changed := sig != l.spool[agentSessionID]
	l.spool[agentSessionID] = sig
	return changed
}
