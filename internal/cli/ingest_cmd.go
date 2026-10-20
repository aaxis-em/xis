package cli

import (
	"encoding/json"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aaxis-em/xis/internal/ingest"
)

func init() { register(newIngestCmd) }

func newIngestCmd() *cobra.Command {
	var session string
	var all, quiet bool
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Ingest the spooled hook events of agent sessions into the database",
		Long: `Ingest reads the spool file and the transcript of a session. It removes secrets,
builds the steps and the edits, and writes the session to the database.
Then it deletes the spool file. The hooks start ingest automatically when a session stops.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := newApp(cmd)
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			in, err := ingest.New(a.Paths, a.Cfg, st)
			if err != nil {
				return userErr(err.Error(), "Check redact.patterns in the config.", err)
			}
			var results []ingest.Result
			var errs []error
			if session != "" && !all {
				r, err := in.IngestSpool(session)
				if err != nil {
					errs = append(errs, err)
				} else {
					results = append(results, r)
				}
			} else {
				results, errs = in.IngestAllPending()
			}
			if quiet {
				if len(errs) > 0 {
					return exitCode(1)
				}
				return nil
			}
			if a.JSON {
				msgs := make([]string, len(errs))
				for i, e := range errs {
					msgs[i] = e.Error()
				}
				return json.NewEncoder(a.Out).Encode(map[string]any{"sessions": results, "errors": msgs})
			}
			if len(results) == 0 && len(errs) == 0 {
				a.printf("nothing to ingest\n")
			}
			for _, r := range results {
				a.printf("ingested %s (agent session %s): %d steps, %d edits, %d test runs\n", shortID(r.SessionID), r.AgentSessionID, r.Steps, r.Edits, r.TestRuns)
				for _, w := range r.Warnings {
					a.printf("  warning: %s\n", w)
				}
			}
			if len(errs) > 0 {
				var b strings.Builder
				for _, e := range errs {
					b.WriteString(e.Error() + "; ")
				}
				return userErr("ingest failed: "+strings.TrimSuffix(b.String(), "; "), "Run with -v for detail. The spool file stays for a retry.", errs[0])
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "agent session ID to ingest")
	cmd.Flags().BoolVar(&all, "all-pending", false, "ingest all spooled sessions (the default)")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "print nothing")
	return cmd
}
