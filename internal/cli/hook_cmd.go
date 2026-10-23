package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aaxis-em/xis/internal/adapters/claudecode"
	"github.com/aaxis-em/xis/internal/adapters/codex"
	"github.com/aaxis-em/xis/internal/capture"
	"github.com/aaxis-em/xis/internal/config"
	"github.com/aaxis-em/xis/internal/xlog"
)

func init() { register(newHookCmd) }

var hookStdin io.Reader = os.Stdin

func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Install, remove or run the agent hooks",
	}
	var agent, scope, command string
	install := &cobra.Command{
		Use:   "install",
		Short: "Add the xis hooks to the Claude Code settings or the Codex hooks",
		Long: `Add the xis hooks to .claude/settings.json (project scope) or ~/.claude/settings.json (user scope).
With --agent codex, add them to .codex/hooks.json or ~/.codex/hooks.json. Codex runs a new hook only
after you approve it in Codex.
The command keeps all other settings and hooks. It does not add a hook twice.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHookInstall(cmd, agent, scope, command, true)
		},
	}
	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the xis hooks from the Claude Code settings or the Codex hooks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHookInstall(cmd, agent, scope, command, false)
		},
	}
	for _, c := range []*cobra.Command{install, uninstall} {
		c.Flags().StringVar(&agent, "agent", claudecode.Agent, "agent: claude-code or codex")
		c.Flags().StringVar(&scope, "scope", "project", "project or user")
	}
	install.Flags().StringVar(&command, "command", "", "command that runs xis in the hook (default: the absolute path of this binary)")
	cmd.AddCommand(install, uninstall, newHookRunCmd(claudecode.Agent, "Claude Code"), newHookRunCmd(codex.Agent, "Codex"))
	return cmd
}

func runHookInstall(cmd *cobra.Command, agent, scope, command string, add bool) error {
	a := newApp(cmd)
	if agent != claudecode.Agent && agent != codex.Agent {
		return userErr("unknown agent "+agent, "Use --agent claude-code or --agent codex.", nil)
	}
	home, _ := os.UserHomeDir()
	if add && scope != "user" && capture.IsHome(a.RepoDir) {
		return userErr("this folder is not in a git repository of its own; the nearest one is your home directory",
			"Run `git init` in the project folder first. To record the sessions in every folder, use --scope user.", nil)
	}
	path, err := capture.SettingsPath(agent, scope, a.RepoDir, home)
	if err != nil {
		return userErr(err.Error(), "Run the command in a repository, or use --scope user.", err)
	}
	var changes []string
	if add {
		if command == "" {
			command = selfCommand()
		}
		changes, err = capture.Install(path, command, agent)
	} else {
		changes, err = capture.Uninstall(path)
	}
	if err != nil {
		return userErr(err.Error(), "Fix the JSON in the settings file and run the command again.", err)
	}
	if a.JSON {
		return json.NewEncoder(a.Out).Encode(map[string]any{"settings": path, "changes": changes})
	}
	if len(changes) == 0 {
		a.printf("%s: no change\n", path)
		return nil
	}
	a.printf("%s:\n", path)
	for _, c := range changes {
		a.printf("  %s\n", c)
	}
	if add && agent == codex.Agent {
		a.printf("Codex runs the hooks only after you approve them: start codex and review the hooks it lists.\n")
	}
	return nil
}

func selfCommand() string {
	exe, err := os.Executable()
	if err != nil || strings.HasSuffix(exe, ".test") || strings.Contains(exe, string(filepath.Separator)+"go-build") {
		return "xis"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return commandPath(exe, runtime.GOOS == "windows")
}

// commandPath quotes the path of the xis binary for a hook command. On
// Windows, Claude Code and git run hooks with Git Bash, which reads a
// backslash as an escape, so the path gets forward slashes. Double quotes
// work in Git Bash and in cmd.exe.
func commandPath(exe string, windows bool) string {
	if windows {
		return `"` + strings.ReplaceAll(exe, `\`, "/") + `"`
	}
	if strings.ContainsAny(exe, " '\"") {
		return "'" + strings.ReplaceAll(exe, "'", `'\''`) + "'"
	}
	return exe
}

func newHookRunCmd(agent, name string) *cobra.Command {
	return &cobra.Command{
		Use:                agent + " EVENT",
		Short:              "Hook entry point for " + name + " (internal)",
		Long:               name + " runs this command for each hook event. It reads the event from stdin. It always exits 0 and never writes to stdout.",
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: true,
		Hidden:             false,
		RunE: func(cmd *cobra.Command, args []string) error {
			event := ""
			if len(args) > 0 {
				event = args[0]
			}
			runHook(agent, event, hookStdin)
			return nil
		},
	}
}

func runHook(agent, event string, stdin io.Reader) {
	p := config.DefaultPaths()
	xlog.Init(p.LogFile, false)
	cfg, err := config.Load(p, findRepo(""))
	if err != nil {
		cfg = config.Default()
	}
	h := capture.NewHandler(p, cfg)
	h.Agent = agent
	h.Handle(event, stdin)
}
