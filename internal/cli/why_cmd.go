package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aaxis-em/xis/internal/attribution"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/store"
)

func init() { register(newWhyCmd) }

type whyStep struct {
	ID   string `json:"id"`
	Seq  int    `json:"seq"`
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type whyResult struct {
	File        string       `json:"file"`
	Line        int          `json:"line"`
	Changed     bool         `json:"changed"`
	SessionID   string       `json:"session_id,omitempty"`
	Agent       string       `json:"agent,omitempty"`
	Fidelity    string       `json:"fidelity,omitempty"`
	HunkID      string       `json:"hunk_id,omitempty"`
	HunkHeader  string       `json:"hunk_header,omitempty"`
	ChangeType  string       `json:"change_type,omitempty"`
	Contributor string       `json:"contributor,omitempty"`
	Step        *whyStep     `json:"step,omitempty"`
	ActorKind   string       `json:"actor_kind,omitempty"`
	Tool        string       `json:"tool,omitempty"`
	Instruction *whyStep     `json:"instruction,omitempty"`
	Intent      []string     `json:"intent_items,omitempty"`
	AgentText   []whyStep    `json:"agent_text_near,omitempty"`
	Evidence    []whyStep    `json:"evidence,omitempty"`
	Rationale   string       `json:"explanation,omitempty"`
	Flags       []model.Flag `json:"flags,omitempty"`
}

func parseFileLine(arg, cwd, repo string) (string, int, error) {
	i := strings.LastIndex(arg, ":")
	if i <= 0 {
		return "", 0, fmt.Errorf("want FILE:LINE, got %q", arg)
	}
	line, err := strconv.Atoi(arg[i+1:])
	if err != nil || line < 1 {
		return "", 0, fmt.Errorf("bad line number in %q", arg)
	}
	file := arg[:i]
	if !filepath.IsAbs(file) {
		file = filepath.Join(cwd, file)
	}
	rel, err := filepath.Rel(repo, file)
	if err != nil || strings.HasPrefix(rel, "..") {
		if r2, err2 := filepath.EvalSymlinks(file); err2 == nil {
			if rr, err3 := filepath.Rel(repo, r2); err3 == nil && !strings.HasPrefix(rr, "..") {
				return filepath.ToSlash(rr), line, nil
			}
		}
		return "", 0, fmt.Errorf("%s is outside the repository", arg)
	}
	return filepath.ToSlash(rel), line, nil
}

// sessionForFile returns the latest session that edited file, or the latest session.
func sessionForFile(st *store.Store, repoID, file string) string {
	sessions, err := st.ListSessions(repoID, 0)
	if err != nil {
		return ""
	}
	for _, s := range sessions {
		edits, _ := st.ListEdits(s.ID)
		for _, e := range edits {
			if e.FilePath == file {
				return s.ID
			}
		}
	}
	return ""
}

func newWhyCmd() *cobra.Command {
	var session string
	cmd := &cobra.Command{
		Use:   "why FILE:LINE",
		Short: "Show where a line came from: session, step, tool call, agent text, intent and flags",
		Long:  "Show the origin of one line of the working tree. The command works without an LLM.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := newApp(cmd)
			if err := a.requireRepo(); err != nil {
				return err
			}
			cwd, _ := os.Getwd()
			repoTop := a.RepoDir
			file, line, err := parseFileLine(args[0], cwd, repoTop)
			if err != nil {
				return userErr(err.Error(), "Example: xis why src/main.go:42", err)
			}
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			a.ingestPending(st)
			e, err := a.engine(st)
			if err != nil {
				return err
			}
			if session == "" {
				session = sessionForFile(st, e.Repo.ID, file)
			}
			res, err := e.Compute(attribution.Options{Mode: attribution.ModeSession, SessionID: session})
			if err != nil {
				return userErr(err.Error(), "Run `xis sessions` to see the sessions.", err)
			}
			w, err := explainLine(st, res, file, line)
			if err != nil {
				return err
			}
			if a.JSON {
				return json.NewEncoder(a.Out).Encode(w)
			}
			printWhy(a, w)
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "session to use (default: the latest session that edited the file)")
	return cmd
}

func toWhyStep(s model.Step) whyStep {
	return whyStep{ID: s.ID, Seq: s.Seq, Kind: string(s.Kind), Text: s.Text}
}

func explainLine(st *store.Store, res *attribution.Result, file string, line int) (*whyResult, error) {
	w := &whyResult{File: file, Line: line}
	h, ok := res.HunkAt(file, line)
	if !ok {
		return w, nil
	}
	sess := res.Sessions[0]
	w.Changed = true
	w.SessionID, w.Agent, w.Fidelity = sess.ID, sess.Agent, string(sess.Fidelity)
	w.HunkID, w.ChangeType = h.ID, string(h.ChangeType)
	w.HunkHeader, _, _ = strings.Cut(h.Patch, "\n")
	w.Contributor = attribution.Contributor(res.ProvenanceOf(h.ID), res.ActorKind)
	if flags, err := st.ListFlags(res.Change.ID); err == nil {
		for _, f := range flags {
			if (f.TargetKind == "hunk" && f.TargetID == h.ID) || (f.TargetKind == "file" && f.TargetID == file) {
				w.Flags = append(w.Flags, f)
			}
		}
	}
	if r, err := st.GetRationale(h.ID); err == nil {
		w.Rationale = r.Explanation
	}
	stepID := res.LineSteps[file][line]
	if stepID == "" {
		for _, p := range res.ProvenanceOf(h.ID) {
			if p.StepID != model.Unattributed {
				stepID = p.StepID
				break
			}
		}
	}
	if stepID == "" || stepID == model.Unattributed {
		return w, nil
	}
	d, err := st.LoadSession(res.StepSession[stepID])
	if err != nil {
		return w, err
	}
	s, _ := d.Step(stepID)
	ws := toWhyStep(s)
	w.Step = &ws
	w.ActorKind = d.ActorKind(stepID)
	if tc, ok := d.ToolCalls[stepID]; ok {
		w.Tool = tc.ToolName
	}
	if ins, ok := d.InstructionFor(stepID); ok {
		x := toWhyStep(ins)
		w.Instruction = &x
		for _, it := range d.Intent {
			if it.SourceStepID == ins.ID {
				w.Intent = append(w.Intent, it.Text)
			}
		}
	}
	for _, t := range d.AgentTextNear(stepID, file, 3) {
		w.AgentText = append(w.AgentText, toWhyStep(t))
	}
	for _, ev := range d.EvidenceBefore(stepID, 5) {
		x := toWhyStep(ev)
		if r, ok := d.TestRunOf(ev.ID); ok {
			x.Text += " → " + strings.ToUpper(r.Outcome)
		}
		w.Evidence = append(w.Evidence, x)
	}
	return w, nil
}

func printWhy(a *App, w *whyResult) {
	a.printf("%s:%d\n", w.File, w.Line)
	if !w.Changed {
		a.printf("  The line is not part of the change of the session. It existed before the session, or no session changed it.\n")
		return
	}
	a.printf("  session      %s · %s (%s)\n", shortID(w.SessionID), w.Agent, w.Fidelity)
	a.printf("  hunk         %s [%s] · contributor %s\n", w.HunkHeader, w.ChangeType, w.Contributor)
	if w.Step == nil {
		a.printf("  step         none: no captured step wrote this line (unattributed)\n")
	} else {
		who := "agent"
		switch w.ActorKind {
		case "human":
			who = "human edit"
		case "subagent":
			who = "subagent"
		}
		a.printf("  step         #%d %s (%s)\n", w.Step.Seq, oneLine(w.Step.Text, 80), who)
	}
	if w.Instruction != nil {
		a.printf("  instruction  #%d %q\n", w.Instruction.Seq, oneLine(w.Instruction.Text, 100))
	}
	for _, it := range w.Intent {
		a.printf("  intent       %q\n", oneLine(it, 100))
	}
	if len(w.AgentText) > 0 {
		a.printf("  agent text near this edit (agent says, unverified):\n")
		for _, t := range w.AgentText {
			a.printf("    #%d %s: %q\n", t.Seq, t.Kind, oneLine(t.Text, 140))
		}
	}
	if len(w.Evidence) > 0 {
		a.printf("  evidence:\n")
		for _, t := range w.Evidence {
			a.printf("    #%d %s\n", t.Seq, oneLine(t.Text, 100))
		}
	}
	if w.Rationale != "" {
		a.printf("  explanation  %s\n", w.Rationale)
	} else {
		a.printf("  explanation  LLM analysis off or not run\n")
	}
	for _, f := range w.Flags {
		sup := ""
		if f.Suppressed {
			sup = " (suppressed)"
		}
		a.printf("  flag         %s %s: %s%s\n", f.Severity, f.RuleID, f.Message, sup)
	}
}
