package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aaxis-em/xis/internal/attribution"
	"github.com/aaxis-em/xis/internal/gitx"
	"github.com/aaxis-em/xis/internal/ingest"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/snapshot"
	"github.com/aaxis-em/xis/internal/store"
)

func (a *App) openStore() (*store.Store, error) {
	if err := a.requireConfig(); err != nil {
		return nil, err
	}
	if err := a.Paths.Ensure(); err != nil {
		return nil, userErr(err.Error(), "Check the permissions of "+a.Paths.DataDir+".", err)
	}
	st, err := store.Open(a.Paths.DB)
	if err != nil {
		return nil, userErr("open database: "+err.Error(), "Run `xis doctor`.", err)
	}
	return st, nil
}

// ingestPending ingests spool files first, so commands see sessions that
// stopped a moment ago.
func (a *App) ingestPending(st *store.Store) {
	in, err := ingest.New(a.Paths, a.Cfg, st)
	if err != nil {
		slog.Warn("ingest", "err", err.Error())
		return
	}
	_, errs := in.IngestAllPending()
	for _, e := range errs {
		slog.Warn("ingest pending", "err", e.Error())
	}
}

func (a *App) engine(st *store.Store) (*attribution.Engine, error) {
	if err := a.requireRepo(); err != nil {
		return nil, err
	}
	e, err := attribution.NewEngine(a.Paths, a.Cfg, st, a.RepoDir)
	if err != nil {
		if errors.Is(err, snapshot.ErrNotGit) {
			return nil, userErr("not inside a git repository", "Use --repo PATH.", err)
		}
		return nil, userErr(err.Error(), "Run `xis doctor`.", err)
	}
	return e, nil
}

func (a *App) computeChange(st *store.Store, opts attribution.Options) (*attribution.Engine, *attribution.Result, error) {
	a.ingestPending(st)
	e, err := a.engine(st)
	if err != nil {
		return nil, nil, err
	}
	res, err := e.Compute(opts)
	if err != nil {
		if errors.Is(err, attribution.ErrNoSession) {
			return nil, nil, userErr("no xis session for this repository", "Run `xis hook install --agent claude-code`, then use the agent. Or run `xis import codex`.", err)
		}
		return nil, nil, userErr(err.Error(), "Run `xis sessions` to see the sessions.", err)
	}
	return e, res, nil
}

// shortID drops the zero time prefix of a model.StableID, which would make
// every short form the same.
func shortID(id string) string {
	id = strings.TrimPrefix(id, model.StableIDPrefix)
	if len(id) > 10 {
		return id[:10]
	}
	return id
}

func fmtDuration(d time.Duration) string {
	switch {
	case d <= 0:
		return "-"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

func gitRun(repo string, args ...string) (string, error) {
	return gitx.In(repo).Run(args...)
}
