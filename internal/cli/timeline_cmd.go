package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/aaxis-em/xis/internal/attribution"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/store"
)

func init() { register(newTimelineCmd) }

type timelineStep struct {
	Seq        int       `json:"seq"`
	ID         string    `json:"id"`
	Time       time.Time `json:"time"`
	Actor      string    `json:"actor"`
	Depth      int       `json:"depth"`
	Kind       string    `json:"kind"`
	Text       string    `json:"text"`
	Tool       string    `json:"tool,omitempty"`
	OK         *bool     `json:"ok,omitempty"`
	Files      []string  `json:"files,omitempty"`
	Test       string    `json:"test,omitempty"`
	Survive    int       `json:"hunks_survive"`
	Abandoned  bool      `json:"abandoned"`
	HasEdits   bool      `json:"has_edits"`
	Snapshot   string    `json:"snapshot,omitempty"`
	ParentStep string    `json:"parent_step,omitempty"`
}

type timelineData struct {
	Session model.Session  `json:"session"`
	Steps   []timelineStep `json:"steps"`
}

func buildTimeline(st *store.Store, d *store.SessionData, res *attribution.Result) timelineData {
	survive := map[string]int{}
	if res != nil {
		for _, p := range res.Provenance {
			survive[p.StepID]++
		}
	}
	out := timelineData{Session: d.Session}
	for _, s := range d.Steps {
		ts := timelineStep{Seq: s.Seq, ID: s.ID, Time: s.TsStart, Kind: string(s.Kind), Text: s.Text, Survive: survive[s.ID], Snapshot: s.Snapshot, ParentStep: s.ParentStepID}
		a := d.Actors[s.ActorID]
		ts.Actor = a.Kind
		if a.Kind == "subagent" {
			ts.Depth = 1
			ts.Actor = a.Name
		}
		if tc, ok := d.ToolCalls[s.ID]; ok {
			ts.Tool = tc.ToolName
			okv := tc.OK
			ts.OK = &okv
		}
		for _, e := range d.EditsOf(s.ID) {
			ts.Files = append(ts.Files, fmt.Sprintf("%s +%d -%d", e.FilePath, e.Added, e.Removed))
		}
		ts.HasEdits = len(ts.Files) > 0
		ts.Abandoned = ts.HasEdits && res != nil && ts.Survive == 0
		if r, ok := d.TestRunOf(s.ID); ok {
			ts.Test = strings.ToUpper(r.Outcome)
		}
		out.Steps = append(out.Steps, ts)
	}
	return out
}

func newTimelineCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "timeline [SESSION]",
		Short: "Print the steps of a session in time order",
		Long:  "Print the steps of a session (default: the latest session). Subagent steps are indented. An abandoned step made edits that do not survive in the change.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := newApp(cmd)
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			id := ""
			if len(args) == 1 {
				id = args[0]
			}
			var res *attribution.Result
			var sess model.Session
			if a.RepoDir != "" {
				_, r, err := a.computeChange(st, attribution.Options{Mode: attribution.ModeSession, SessionID: id})
				if err == nil {
					res, sess = r, r.Sessions[0]
				}
			}
			if res == nil {
				a.ingestPending(st)
				if sess, err = attribution.ResolveSession(st, "", id); err != nil {
					return userErr(err.Error(), "Run `xis sessions` to see the sessions.", err)
				}
			}
			d, err := st.LoadSession(sess.ID)
			if err != nil {
				return err
			}
			tl := buildTimeline(st, d, res)
			if a.JSON {
				return json.NewEncoder(a.Out).Encode(tl)
			}
			printTimeline(a, tl)
			return nil
		},
	}
}

func printTimeline(a *App, tl timelineData) {
	s := tl.Session
	a.printf("session %s · %s %s (%s) · %d steps · %s\n", shortID(s.ID), s.Agent, s.AgentVersion, s.Fidelity, len(tl.Steps), s.StartedAt.Local().Format("2006-01-02 15:04"))
	dim := func(t string) string {
		if a.NoColor {
			return t
		}
		return "\x1b[2m" + t + "\x1b[0m"
	}
	for _, t := range tl.Steps {
		off := t.Time.Sub(s.StartedAt).Round(time.Second)
		indent := strings.Repeat("  ", t.Depth)
		label := t.Kind
		if t.Tool != "" {
			label = "tool"
		}
		status := ""
		if t.OK != nil && !*t.OK {
			status = " ✗"
		}
		if t.Test != "" {
			status += " [test " + t.Test + "]"
		}
		line := fmt.Sprintf("%4d %7s %s%-9s %s%s", t.Seq, "+"+fmtDuration(off), indent, label, oneLine(t.Text, 90), status)
		if t.Depth > 0 {
			line += "  (" + t.Actor + ")"
		}
		if t.HasEdits {
			line += fmt.Sprintf("  → %s · %d hunks survive", strings.Join(t.Files, ", "), t.Survive)
			if t.Abandoned {
				line += " (abandoned)"
			}
		}
		if t.Abandoned {
			line = dim(line)
		}
		a.printf("%s\n", line)
	}
}
