package cli

import (
	"encoding/json"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/aaxis-em/xis/internal/adapters/codex"
	"github.com/aaxis-em/xis/internal/ingest"
)

func init() { register(newImportCmd) }

func newImportCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "import", Short: "Import agent sessions from their logs (lower fidelity than live capture)"}
	var path, since, base string
	var latest bool
	cx := &cobra.Command{
		Use:   "codex",
		Short: "Import Codex CLI rollout files from ~/.codex/sessions",
		Long: `Import Codex CLI rollouts. An import has no snapshots, so the fidelity is "imported". For live capture, run
` + "`xis hook install --agent codex`" + ` instead.
xis attributes a hunk to an apply_patch step only when the patch added the same lines. Hunks that only
shell commands could have made stay unattributed. xis never guesses.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := newApp(cmd)
			var files []string
			switch {
			case path != "":
				files = []string{path}
			default:
				all, err := ingest.FindRollouts(codex.Home())
				if err != nil {
					return userErr(err.Error(), "", err)
				}
				if len(all) == 0 {
					return userErr("no Codex rollouts in "+codex.Home()+"/sessions", "Use --path FILE, or set CODEX_HOME.", nil)
				}
				type f struct {
					p string
					t time.Time
				}
				var fs []f
				var cutoff time.Time
				if since != "" {
					t, err := time.ParseInLocation("2006-01-02", since, time.Local)
					if err != nil {
						return userErr("bad --since date "+since, "Use YYYY-MM-DD.", err)
					}
					cutoff = t
				}
				for _, p := range all {
					fi, err := os.Stat(p)
					if err == nil && !fi.ModTime().Before(cutoff) {
						fs = append(fs, f{p, fi.ModTime()})
					}
				}
				sort.Slice(fs, func(i, j int) bool { return fs[i].t.After(fs[j].t) })
				if since == "" || latest {
					fs = fs[:min(1, len(fs))]
				}
				for _, x := range fs {
					files = append(files, x.p)
				}
			}
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			in, err := ingest.New(a.Paths, a.Cfg, st)
			if err != nil {
				return err
			}
			var results []ingest.Result
			for _, p := range files {
				r, err := in.ImportCodex(p, a.RepoDir, base)
				if err != nil {
					return userErr(p+": "+err.Error(), "Use --repo PATH for the repository of the session, and --base REF for its base commit.", err)
				}
				results = append(results, r)
			}
			if a.JSON {
				return json.NewEncoder(a.Out).Encode(results)
			}
			for _, r := range results {
				a.printf("imported %s (codex session %s): %d steps, %d edits, %d test runs (fidelity: imported)\n", shortID(r.SessionID), r.AgentSessionID, r.Steps, r.Edits, r.TestRuns)
				for _, w := range r.Warnings {
					a.printf("  warning: %s\n", w)
				}
			}
			return nil
		},
	}
	cx.Flags().StringVar(&path, "path", "", "rollout file to import")
	cx.Flags().BoolVar(&latest, "latest", false, "import the newest rollout (the default)")
	cx.Flags().StringVar(&since, "since", "", "import all rollouts changed on or after this date (YYYY-MM-DD)")
	cx.Flags().StringVar(&base, "base", "", "base commit of the session (default: the commit in the rollout, else HEAD)")
	cmd.AddCommand(cx)
	return cmd
}
