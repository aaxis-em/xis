package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aaxis-em/xis/internal/analysis"
	"github.com/aaxis-em/xis/internal/llm"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/qa"
	"github.com/aaxis-em/xis/internal/redact"
	"github.com/aaxis-em/xis/internal/store"
	"github.com/aaxis-em/xis/internal/textx"
)

func init() {
	register(newAnalyzeCmd)
	register(newAskCmd)
}

// newProvider is a variable so tests can install the mock provider.
var newProvider = llm.New

func (a *App) llmClient() (*llm.Client, error) {
	p, err := newProvider(a.Cfg.LLM)
	if errors.Is(err, llm.ErrOff) {
		return nil, userErr("LLM analysis is off", "Set ANTHROPIC_API_KEY, or set llm.provider (anthropic or openaicompat). `xis why` and `xis flags` work without an LLM.", err)
	}
	if err != nil {
		return nil, userErr("LLM provider: "+err.Error(), "Check the [llm] section of the config.", err)
	}
	return &llm.Client{Provider: p, Cache: llm.Cache{Dir: a.Paths.LLMCache}, Budget: llm.NewBudget(a.Cfg.LLM)}, nil
}

func (a *App) redactFunc() (func(string) string, error) {
	r, err := redact.New(a.Cfg.Redact)
	if err != nil {
		return nil, err
	}
	return r.String, nil
}

func analysisInput(ca *changeAnalysis) analysis.Input {
	in := analysis.Input{Change: ca.Result.Change, Hunks: ca.Result.Hunks, Provenance: map[string][]model.HunkProvenance{}, Sessions: ca.Sessions, Units: ca.Result.Units}
	for _, p := range ca.Result.Provenance {
		in.Provenance[p.HunkID] = append(in.Provenance[p.HunkID], p)
	}
	return in
}

func saveAnalysis(st *store.Store, ca *changeAnalysis, out *analysis.Output) error {
	return st.Tx(func(tx *store.Store) error {
		if len(out.Intent) > 0 {
			for _, d := range ca.Sessions {
				if err := tx.DeleteIntentItems(d.Session.ID); err != nil {
					return err
				}
			}
		}
		for _, it := range out.Intent {
			if err := tx.PutIntentItem(it); err != nil {
				return err
			}
		}
		if !out.GroupingFallback && len(out.Units) > 0 {
			keep := make([]string, len(out.Units))
			for i, u := range out.Units {
				keep[i] = u.ID
				if err := tx.PutReviewUnit(u); err != nil {
					return err
				}
			}
			for hid, uid := range out.HunkUnit {
				if err := tx.SetHunkUnit(hid, uid); err != nil {
					return err
				}
			}
			if err := tx.DeleteReviewUnitsExcept(ca.Result.Change.ID, keep); err != nil {
				return err
			}
		}
		for _, r := range out.Rationales {
			if err := tx.PutRationale(r); err != nil {
				return err
			}
		}
		for _, hi := range out.HunkIntent {
			if err := tx.PutHunkIntent(hi); err != nil {
				return err
			}
		}
		for id, s := range out.StepSummaries {
			if err := tx.SetStepSummary(id, s); err != nil {
				return err
			}
		}
		return nil
	})
}

func newAnalyzeCmd() *cobra.Command {
	var cf changeFlags
	cmd := &cobra.Command{
		Use:   "analyze [SESSION]",
		Short: "Run the LLM analysis of a change: intent, review units and hunk explanations",
		Long: `Run the LLM analysis of a change. xis removes secrets before each call and caches the responses.
Every statement of the LLM must cite steps or hunks. xis deletes sentences without a valid citation.
The analysis stops when the estimated cost gets to llm.max_usd_per_change.`,
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
			client, err := a.llmClient()
			if err != nil {
				return err
			}
			red, err := a.redactFunc()
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
			an := &analysis.Analyzer{LLM: client, Redact: red, SendCode: a.Cfg.LLM.SendCode}
			out, runErr := an.Run(context.Background(), analysisInput(ca))
			if out != nil {
				if err := saveAnalysis(st, ca, out); err != nil {
					return err
				}
			}
			b := client.Budget
			if a.JSON {
				return json.NewEncoder(a.Out).Encode(map[string]any{
					"change_id": ca.Result.Change.ID, "intent": out.Intent, "units": out.Units, "grouping_fallback": out.GroupingFallback,
					"rationales": len(out.Rationales), "model": out.Model, "calls": b.Calls, "cache_hits": b.CacheHits,
					"estimated_usd": b.SpentUSD, "warnings": out.Warnings, "error": errString(runErr),
				})
			}
			a.printf("model %s · %d calls · %d cached · estimated $%.4f\n", out.Model, b.Calls, b.CacheHits, b.SpentUSD)
			a.printf("intent items: %d\n", len(out.Intent))
			for i, it := range out.Intent {
				a.printf("  #%d [%s] %s\n", i+1, it.Status, it.Text)
			}
			if out.GroupingFallback || len(out.Units) == 0 {
				a.printf("review units: fixed grouping (%d units)\n", len(ca.Result.Units))
			} else {
				a.printf("review units: %d (LLM)\n", len(out.Units))
				for _, u := range out.Units {
					a.printf("  %d %s\n", u.Ord, u.Title)
				}
			}
			a.printf("hunk explanations: %d of %d hunks\n", len(out.Rationales), len(ca.Result.Hunks))
			for _, w := range out.Warnings {
				a.printf("warning: %s\n", w)
			}
			if runErr != nil {
				return userErr(runErr.Error(), "Raise llm.max_usd_per_change, or run again: cached answers cost nothing.", runErr)
			}
			return nil
		},
	}
	cf.add(cmd)
	return cmd
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (a *App) asker() (*qa.Asker, error) {
	client, err := a.llmClient()
	if err != nil {
		return nil, err
	}
	red, err := a.redactFunc()
	if err != nil {
		return nil, err
	}
	return &qa.Asker{LLM: client, Redact: red, SendCode: a.Cfg.LLM.SendCode}, nil
}

func intentOf(ca *changeAnalysis) []model.IntentItem {
	var out []model.IntentItem
	for _, d := range ca.Sessions {
		out = append(out, d.Intent...)
	}
	return out
}

func (a *App) askFunc(ca *changeAnalysis) func(string) (string, error) {
	asker, err := a.asker()
	if err != nil {
		return nil
	}
	return func(q string) (string, error) {
		res, err := asker.Ask(context.Background(), analysisInput(ca), intentOf(ca), q)
		if err != nil {
			return "", err
		}
		return res.Answer, nil
	}
}

func newAskCmd() *cobra.Command {
	var cf changeFlags
	var session string
	cmd := &cobra.Command{
		Use:   `ask "QUESTION"`,
		Short: "Ask a question about a change; the answer cites steps and hunks",
		Long: `Ask a question about a change. The answer uses only the session record and cites [step:ID] and [hunk:ID].
If the record has no answer, the answer says "Not in the session record."`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := newApp(cmd)
			opts, err := cf.options(session)
			if err != nil {
				return err
			}
			asker, err := a.asker()
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
			res, err := asker.Ask(context.Background(), analysisInput(ca), intentOf(ca), strings.Join(args, " "))
			if err != nil {
				return userErr(err.Error(), "Check the [llm] section of the config.", err)
			}
			if a.JSON {
				return json.NewEncoder(a.Out).Encode(res)
			}
			a.printf("%s\n", res.Answer)
			if len(res.CitedStepIDs)+len(res.CitedHunkIDs) > 0 {
				a.printf("\ncitations:\n")
				for _, id := range res.CitedStepIDs {
					for _, d := range ca.Sessions {
						if s, ok := d.Step(id); ok {
							a.printf("  [step:%s] step %d %s\n", id, s.Seq, textx.OneLine(s.Text, 80))
						}
					}
				}
				for _, id := range res.CitedHunkIDs {
					for _, h := range ca.Result.Hunks {
						if h.ID == id {
							a.printf("  [hunk:%s] %s:%d\n", id, h.FilePath, h.NewStart)
						}
					}
				}
			}
			return nil
		},
	}
	cf.add(cmd)
	cmd.Flags().StringVar(&session, "session", "", "session (default: the latest session)")
	return cmd
}
