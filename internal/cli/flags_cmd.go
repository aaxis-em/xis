package cli

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/aaxis-em/xis/internal/attribution"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/redact"
	"github.com/aaxis-em/xis/internal/rules"
	"github.com/aaxis-em/xis/internal/store"
	"github.com/aaxis-em/xis/internal/textx"
)

func init() { register(newFlagsCmd) }

type changeFlags struct {
	staged bool
	rng    string
	pr     bool
}

func (f *changeFlags) add(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.staged, "staged", false, "review the staged changes (base: HEAD, target: the index)")
	cmd.Flags().StringVar(&f.rng, "range", "", "review a commit range A..B")
	cmd.Flags().BoolVar(&f.pr, "pr", false, "review the branch (base: merge-base with the default branch, target: HEAD)")
}

func (f *changeFlags) options(session string) (attribution.Options, error) {
	n := 0
	for _, b := range []bool{f.staged, f.rng != "", f.pr} {
		if b {
			n++
		}
	}
	if n > 1 {
		return attribution.Options{}, userErr("use only one of --staged, --range and --pr", "", nil)
	}
	switch {
	case f.staged:
		return attribution.Options{Mode: attribution.ModeStaged}, nil
	case f.rng != "":
		return attribution.Options{Mode: attribution.ModeRange, Range: f.rng}, nil
	case f.pr:
		return attribution.Options{Mode: attribution.ModePR}, nil
	}
	return attribution.Options{Mode: attribution.ModeSession, SessionID: session}, nil
}

type changeAnalysis struct {
	Engine   *attribution.Engine
	Result   *attribution.Result
	Sessions []*store.SessionData
	Flags    []model.Flag
}

// analyzeChange computes the change, runs the rules and stores the flags.
func (a *App) analyzeChange(st *store.Store, opts attribution.Options) (*changeAnalysis, error) {
	e, res, err := a.computeChange(st, opts)
	if err != nil {
		return nil, err
	}
	ca := &changeAnalysis{Engine: e, Result: res}
	for _, s := range res.Sessions {
		d, err := st.LoadSession(s.ID)
		if err != nil {
			return nil, err
		}
		ca.Sessions = append(ca.Sessions, d)
	}
	red, err := redact.New(a.Cfg.Redact)
	if err != nil {
		return nil, err
	}
	flags := rules.Run(rules.NewContext(res, ca.Sessions, e.Repo.Top, a.Cfg, red))
	err = st.Tx(func(tx *store.Store) error {
		if err := tx.DeleteFlags(res.Change.ID, "rule"); err != nil {
			return err
		}
		for _, f := range flags {
			if err := tx.PutFlag(f); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	ca.Flags, err = st.ListFlags(res.Change.ID)
	return ca, err
}

func (ca *changeAnalysis) location(f model.Flag) string {
	switch f.TargetKind {
	case "file":
		return f.TargetID
	case "hunk":
		for _, h := range ca.Result.Hunks {
			if h.ID == f.TargetID {
				return fmt.Sprintf("%s:%d", h.FilePath, cmp.Or(f.Line, h.NewStart))
			}
		}
	case "step":
		for _, d := range ca.Sessions {
			if s, ok := d.Step(f.TargetID); ok {
				return fmt.Sprintf("step %d", s.Seq)
			}
		}
	case "session":
		return "session " + shortID(f.TargetID)
	}
	return f.TargetKind + " " + shortID(f.TargetID)
}

type flagJSON struct {
	model.Flag
	Location string `json:"location"`
}

func newFlagsCmd() *cobra.Command {
	var cf changeFlags
	var failOn, session string
	cmd := &cobra.Command{
		Use:   "flags [SESSION]",
		Short: "Run the rules on a change and print the flags",
		Long: `Run the fixed rules on a change and print the flags. The rules need no LLM.
Exit codes: 0 = no unsuppressed flag at or above --fail-on, 1 = flags found, 2 = xis error.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				session = args[0]
			}
			err := runFlags(cmd, cf, failOn, session)
			var ec exitCode
			if err == nil || errors.As(err, &ec) {
				return err
			}
			var xe *Error
			if errors.As(err, &xe) {
				xe.Code = 2
				return xe
			}
			return &Error{Msg: err.Error(), Code: 2, Err: err}
		},
	}
	cf.add(cmd)
	cmd.Flags().StringVar(&failOn, "fail-on", "high", "lowest severity that sets exit code 1: info, low, medium or high")
	cmd.AddCommand(newSuppressCmd())
	return cmd
}

func runFlags(cmd *cobra.Command, cf changeFlags, failOn, session string) error {
	a := newApp(cmd)
	threshold := model.Severity(strings.ToLower(failOn))
	if threshold.Rank() < 0 {
		return userErr("unknown severity "+failOn, "Use info, low, medium or high.", nil)
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
	failing := 0
	for _, f := range ca.Flags {
		if !f.Suppressed && f.Severity.Rank() >= threshold.Rank() {
			failing++
		}
	}
	if a.JSON {
		out := make([]flagJSON, 0, len(ca.Flags))
		for _, f := range ca.Flags {
			out = append(out, flagJSON{f, ca.location(f)})
		}
		if err := json.NewEncoder(a.Out).Encode(map[string]any{"change_id": ca.Result.Change.ID, "fail_on": threshold, "failing": failing, "flags": out}); err != nil {
			return err
		}
	} else {
		printFlags(a, ca)
	}
	if failing > 0 {
		return exitCode(1)
	}
	return nil
}

func printFlags(a *App, ca *changeAnalysis) {
	if len(ca.Flags) == 0 {
		a.printf("no flags\n")
		return
	}
	tw := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SEVERITY\tRULE\tLOCATION\tMESSAGE\tID")
	var suppressed []model.Flag
	for _, f := range ca.Flags {
		if f.Suppressed {
			suppressed = append(suppressed, f)
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", f.Severity, f.RuleID, ca.location(f), textx.OneLine(f.Message, 90), shortID(f.ID))
	}
	_ = tw.Flush()
	if len(suppressed) > 0 {
		a.printf("\nsuppressed:\n")
		for _, f := range suppressed {
			a.printf("  %s %s %s: %s (reason: %s)\n", f.Severity, f.RuleID, ca.location(f), textx.OneLine(f.Message, 70), f.SuppressReason)
		}
	}
}

func newSuppressCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "suppress FLAG_ID",
		Short: "Suppress one flag (and the same finding in later runs) with a reason",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := newApp(cmd)
			if strings.TrimSpace(reason) == "" {
				return userErr("a reason is necessary", `Add --reason "why this flag is acceptable".`, nil)
			}
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			f, err := st.FindFlag(args[0])
			if errors.Is(err, store.ErrNotFound) {
				return userErr("flag "+args[0]+" not found", "Run `xis flags` to see the flag IDs.", err)
			}
			if err != nil {
				return userErr(err.Error(), "Give more characters of the flag ID.", err)
			}
			f, err = st.SuppressFlag(f.ID, reason)
			if err != nil {
				return err
			}
			if a.JSON {
				return json.NewEncoder(a.Out).Encode(f)
			}
			a.printf("suppressed %s %s: %s\n", f.RuleID, shortID(f.ID), reason)
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why the flag is acceptable (shown in reports)")
	return cmd
}
