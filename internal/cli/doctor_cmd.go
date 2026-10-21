package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/aaxis-em/xis/internal/adapters/claudecode"
	"github.com/aaxis-em/xis/internal/capture"
	"github.com/aaxis-em/xis/internal/config"
	"github.com/aaxis-em/xis/internal/gitx"
	"github.com/aaxis-em/xis/internal/notes"
	"github.com/aaxis-em/xis/internal/store"
)

func init() { register(newDoctorCmd) }

type check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Hint   string `json:"hint,omitempty"`
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check git, hooks, permissions, the database, the LLM config and the spool",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := newApp(cmd)
			checks := runDoctor(a)
			failed := false
			for _, c := range checks {
				if c.Status == "fail" {
					failed = true
				}
			}
			if a.JSON {
				if err := json.NewEncoder(a.Out).Encode(checks); err != nil {
					return err
				}
			} else {
				for _, c := range checks {
					a.printf("%-4s  %-14s %s\n", c.Status, c.Name, c.Detail)
					if c.Hint != "" && c.Status != "ok" {
						a.printf("      %-14s %s\n", "", c.Hint)
					}
				}
			}
			if failed {
				return exitCode(1)
			}
			return nil
		},
	}
}

func runDoctor(a *App) []check {
	var out []check
	add := func(name, status, detail, hint string) {
		out = append(out, check{name, status, detail, hint})
	}

	if v, err := gitx.Version(); err != nil {
		add("git", "fail", "git not found", "Install git 2.31 or later.")
	} else if !gitAtLeast(v, 2, 31) {
		add("git", "fail", "git "+v, "Install git 2.31 or later.")
	} else {
		add("git", "ok", "git "+v, "")
	}

	if a.cfgErr != nil {
		add("config", "fail", a.cfgErr.Error(), "Fix the config file. Run `xis config path` to find it.")
	} else {
		add("config", "ok", a.Paths.ConfigFile, "")
	}

	add(checkPerms(a.Paths))

	if _, err := os.Stat(a.Paths.DB); os.IsNotExist(err) {
		add("database", "warn", a.Paths.DB+" does not exist", "Run `xis init`.")
	} else if db, err := store.Open(a.Paths.DB); err != nil {
		add("database", "fail", err.Error(), "Run `xis init`.")
	} else {
		if err := db.Check(); err != nil {
			add("database", "fail", err.Error(), "Move the database file away and run `xis ingest --all-pending`.")
		} else {
			ms, _ := db.Migrations()
			sessions, _ := db.ListSessions("", 0)
			add("database", "ok", fmt.Sprintf("%s (%d migrations, %d sessions)", a.Paths.DB, len(ms), len(sessions)), "")
		}
		db.Close()
	}

	home, _ := os.UserHomeDir()
	var installedIn []string
	missing := 0
	for _, scope := range []string{"project", "user"} {
		path, err := capture.SettingsPath(scope, a.RepoDir, home)
		if err != nil {
			continue
		}
		inst, err := capture.Installed(path)
		if err != nil {
			add("hooks", "warn", fmt.Sprintf("cannot read %s: %v", path, err), "")
			continue
		}
		if len(inst) > 0 {
			installedIn = append(installedIn, scope)
			missing = len(claudecode.Events) - len(inst)
		}
	}
	switch {
	case len(installedIn) == 0:
		add("hooks", "warn", "no xis hooks in the Claude Code settings", "Run `xis hook install --agent claude-code`.")
	case missing > 0:
		add("hooks", "warn", fmt.Sprintf("%d hook events are missing (%s)", missing, strings.Join(installedIn, ", ")), "Run `xis hook install` again.")
	default:
		add("hooks", "ok", "installed ("+strings.Join(installedIn, ", ")+")", "")
	}

	if a.RepoDir != "" {
		if dir, err := gitHooksDir(a.RepoDir); err == nil {
			b, err := os.ReadFile(filepath.Join(dir, "pre-commit"))
			switch {
			case err == nil && strings.Contains(string(b), gitHookMarker):
				add("pre-commit", "ok", "installed", "")
			default:
				add("pre-commit", "info", "not installed (optional)", "Run `xis precommit install` to block commits with high-severity flags.")
			}
		}
	}

	files, err := capture.ListSpool(a.Paths.Spool)
	switch {
	case err != nil:
		add("spool", "warn", err.Error(), "")
	case len(files) == 0:
		add("spool", "ok", "empty", "")
	default:
		oldest := time.Since(files[0].ModTime).Round(time.Minute)
		status := "ok"
		if oldest > time.Hour {
			status = "warn"
		}
		add("spool", status, fmt.Sprintf("%d sessions wait for ingest (oldest %s)", len(files), oldest), "Run `xis ingest --all-pending`.")
	}

	switch p := a.Cfg.LLM.EffectiveProvider(); p {
	case "none":
		add("llm", "ok", "off (provider none). Rules, why and flags work without it.", "")
	case "anthropic":
		if os.Getenv(a.Cfg.LLM.APIKeyEnv) == "" {
			add("llm", "warn", "provider anthropic, but "+a.Cfg.LLM.APIKeyEnv+" is not set", "Set the variable, or set llm.provider to none.")
		} else {
			add("llm", "ok", "anthropic, model "+a.Cfg.LLM.Model, "")
		}
	case "openaicompat":
		if a.Cfg.LLM.BaseURL == "" {
			add("llm", "warn", "provider openaicompat, but llm.base_url is empty", "Set llm.base_url, for example http://localhost:11434/v1.")
		} else {
			add("llm", "ok", "openaicompat "+a.Cfg.LLM.BaseURL+", model "+a.Cfg.LLM.Model, "")
		}
	default:
		add("llm", "ok", p, "")
	}

	if notes.KeyExists(a.Paths.Keys) {
		add("signing key", "ok", a.Paths.Keys, "")
	} else {
		add("signing key", "warn", "no signing key", "Run `xis init`.")
	}
	return out
}

func checkPerms(p config.Paths) (string, string, string, string) {
	if _, err := os.Stat(p.DataDir); os.IsNotExist(err) {
		return "permissions", "warn", p.DataDir + " does not exist", "Run `xis init`."
	}
	var bad []string
	_ = filepath.WalkDir(p.DataDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && filepath.Ext(path) == ".git" {
			return filepath.SkipDir
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		mode := info.Mode().Perm()
		if mode&0o077 != 0 {
			bad = append(bad, fmt.Sprintf("%s (%o)", path, mode))
		}
		return nil
	})
	if len(bad) > 0 {
		return "permissions", "warn", fmt.Sprintf("%d paths are readable by other users, for example %s", len(bad), bad[0]), "Run `chmod -R go-rwx " + p.DataDir + "`."
	}
	return "permissions", "ok", p.DataDir + " (0700/0600)", ""
}

func gitAtLeast(v string, major, minor int) bool {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return false
	}
	ma, err1 := strconv.Atoi(parts[0])
	mi, err2 := strconv.Atoi(strings.TrimFunc(parts[1], func(r rune) bool { return r < '0' || r > '9' }))
	if err1 != nil || err2 != nil {
		return false
	}
	return ma > major || (ma == major && mi >= minor)
}
