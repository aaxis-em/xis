package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/aaxis-em/xis/internal/capture"
	"github.com/aaxis-em/xis/internal/snapshot"
)

func init() { register(newGCCmd) }

type gcResult struct {
	SnapshotSessions int      `json:"snapshot_sessions_removed"`
	ShadowRepos      int      `json:"shadow_repos_collected"`
	SpoolFiles       int      `json:"spool_files_removed"`
	CacheFiles       int      `json:"llm_cache_files_removed"`
	Errors           []string `json:"errors,omitempty"`
}

func newGCCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Apply the retention settings: remove old snapshots, failed spool files and old LLM cache files",
		Long: `Remove the snapshots of sessions older than retention.snapshots_days (default 30 days), then run git gc on the shadow repositories.
Remove spool files that ingest did not finish within retention.raw_days (default 7 days).
Remove LLM cache files older than retention.snapshots_days.
The database keeps the sessions. Old sessions lose their line attribution, because the snapshots are gone.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := newApp(cmd)
			st, err := a.openStore()
			if err != nil {
				return err
			}
			defer st.Close()
			var res gcResult
			now := time.Now()
			snapCut := now.AddDate(0, 0, -max(a.Cfg.Retention.SnapshotsDays, 1))
			old, err := st.SessionsBefore(snapCut)
			if err != nil {
				return err
			}
			byRepo := map[string][]string{}
			for _, s := range old {
				if s.FinalSnapshot != "" || s.BaseSnapshot != "" {
					byRepo[s.RepoID] = append(byRepo[s.RepoID], s.AgentSessionID)
				}
			}
			entries, _ := os.ReadDir(a.Paths.Shadow)
			for _, e := range entries {
				if !e.IsDir() || filepath.Ext(e.Name()) != ".git" {
					continue
				}
				id := e.Name()[:len(e.Name())-4]
				sh := &snapshot.Shadow{Path: filepath.Join(a.Paths.Shadow, e.Name())}
				for _, sid := range byRepo[id] {
					if sh.Head(sid) == "" {
						continue
					}
					res.SnapshotSessions++
					if !dryRun {
						if err := sh.DeleteSession(sid); err != nil {
							res.Errors = append(res.Errors, err.Error())
						}
					}
				}
				if !dryRun {
					if err := sh.GC(); err != nil {
						res.Errors = append(res.Errors, e.Name()+": "+err.Error())
					}
				}
				res.ShadowRepos++
			}
			rawCut := now.AddDate(0, 0, -max(a.Cfg.Retention.RawDays, 1))
			files, _ := capture.ListSpool(a.Paths.Spool)
			for _, f := range files {
				if f.ModTime.Before(rawCut) {
					res.SpoolFiles++
					if !dryRun {
						_ = capture.RemoveSpool(a.Paths.Spool, f.SessionID)
					}
				}
			}
			_ = filepath.WalkDir(a.Paths.LLMCache, func(p string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				if info, err := d.Info(); err == nil && info.ModTime().Before(snapCut) {
					res.CacheFiles++
					if !dryRun {
						_ = os.Remove(p)
					}
				}
				return nil
			})
			if a.JSON {
				return json.NewEncoder(a.Out).Encode(res)
			}
			verb := "removed"
			if dryRun {
				verb = "would remove"
			}
			a.printf("%s snapshots of %d sessions (older than %d days)\n", verb, res.SnapshotSessions, a.Cfg.Retention.SnapshotsDays)
			a.printf("%s %d spool files (older than %d days)\n", verb, res.SpoolFiles, a.Cfg.Retention.RawDays)
			a.printf("%s %d LLM cache files\n", verb, res.CacheFiles)
			if !dryRun {
				a.printf("ran git gc on %d shadow repositories\n", res.ShadowRepos)
			}
			for _, e := range res.Errors {
				a.printf("error: %s\n", e)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what gc would remove, and remove nothing")
	return cmd
}
