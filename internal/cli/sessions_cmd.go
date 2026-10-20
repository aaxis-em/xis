package cli

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/aaxis-em/xis/internal/snapshot"
)

func init() { register(newSessionsCmd) }

type sessionRow struct {
	ID             string         `json:"id"`
	AgentSessionID string         `json:"agent_session_id"`
	Agent          string         `json:"agent"`
	Fidelity       string         `json:"fidelity"`
	Status         string         `json:"status"`
	StartedAt      time.Time      `json:"started_at"`
	DurationSec    int64          `json:"duration_s"`
	Steps          int            `json:"steps"`
	FilesChanged   int            `json:"files_changed"`
	Flags          map[string]int `json:"flags"`
	Title          string         `json:"title"`
}

func newSessionsCmd() *cobra.Command {
	var limit int
	var allRepos bool
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List the sessions of this repository",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := newApp(cmd)
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			a.ingestPending(st)
			repoID := ""
			if !allRepos && a.RepoDir != "" {
				if r, err := snapshot.Discover(a.RepoDir); err == nil {
					repoID = r.ID
				}
			}
			sessions, err := st.ListSessions(repoID, limit)
			if err != nil {
				return err
			}
			var rows []sessionRow
			for _, s := range sessions {
				steps, _ := st.ListSteps(s.ID)
				edits, _ := st.ListEdits(s.ID)
				files := map[string]bool{}
				for _, e := range edits {
					files[e.FilePath] = true
				}
				counts, _ := st.FlagCountsBySession(s.ID)
				rows = append(rows, sessionRow{
					ID: s.ID, AgentSessionID: s.AgentSessionID, Agent: s.Agent, Fidelity: string(s.Fidelity), Status: s.Status,
					StartedAt: s.StartedAt, DurationSec: int64(s.EndedAt.Sub(s.StartedAt).Seconds()), Steps: len(steps),
					FilesChanged: len(files), Flags: counts, Title: s.Title,
				})
			}
			if a.JSON {
				if rows == nil {
					rows = []sessionRow{}
				}
				return json.NewEncoder(a.Out).Encode(rows)
			}
			if len(rows) == 0 {
				a.printf("no sessions\n")
				return nil
			}
			tw := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tAGENT\tFIDELITY\tSTARTED\tDURATION\tSTEPS\tFILES\tFLAGS (H/M/L)\tSTATUS")
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%d\t%d/%d/%d\t%s\n", shortID(r.ID), r.Agent, r.Fidelity,
					r.StartedAt.Local().Format("2006-01-02 15:04"), fmtDuration(time.Duration(r.DurationSec)*time.Second),
					r.Steps, r.FilesChanged, r.Flags["high"], r.Flags["medium"], r.Flags["low"], r.Status)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum number of sessions (0: all)")
	cmd.Flags().BoolVar(&allRepos, "all", false, "list the sessions of all repositories")
	return cmd
}
