package cli

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aaxis-em/xis/internal/config"
	"github.com/aaxis-em/xis/internal/xlog"
)

type Error struct {
	Msg  string
	Hint string
	Code int
	Err  error
}

func (e *Error) Error() string { return e.Msg }
func (e *Error) Unwrap() error { return e.Err }

func userErr(msg, hint string, err error) *Error {
	return &Error{Msg: msg, Hint: hint, Code: 1, Err: err}
}

type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit %d", int(e)) }

type globals struct {
	repo    string
	json    bool
	noColor bool
	verbose bool
}

type App struct {
	Paths   config.Paths
	Cfg     config.Config
	RepoDir string
	Out     io.Writer
	Err     io.Writer
	JSON    bool
	NoColor bool
	Verbose bool
	cfgErr  error
}

var g globals

func newApp(cmd *cobra.Command) *App {
	a := &App{
		Paths:   config.DefaultPaths(),
		Out:     cmd.OutOrStdout(),
		Err:     cmd.ErrOrStderr(),
		JSON:    g.json,
		NoColor: g.noColor || os.Getenv("NO_COLOR") != "",
		Verbose: g.verbose,
	}
	a.RepoDir = findRepo(g.repo)
	a.Cfg, a.cfgErr = config.Load(a.Paths, a.RepoDir)
	if a.cfgErr == nil {
		a.cfgErr = a.Cfg.Validate()
	}
	xlog.Init(a.Paths.LogFile, a.Verbose)
	return a
}

func (a *App) requireConfig() error {
	if a.cfgErr != nil {
		return userErr("config: "+a.cfgErr.Error(), "Run `xis config path` to find the config file.", a.cfgErr)
	}
	return nil
}

func (a *App) requireRepo() error {
	if a.RepoDir == "" {
		return userErr("not inside a git repository", "Run the command in a repository, or use --repo PATH.", nil)
	}
	return nil
}

func findRepo(start string) string {
	dir := start
	if dir == "" {
		dir, _ = os.Getwd()
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func (a *App) printf(format string, args ...any) { fmt.Fprintf(a.Out, format, args...) }

func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "xis",
		Short: "Show what an AI coding agent did to your code and why",
		Long: `xis links each changed hunk to the agent step that made it.
It shows the instruction, the agent text and the evidence for each hunk.
It flags risky behaviour with fixed rules. The rules need no LLM.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	pf := root.PersistentFlags()
	pf.StringVar(&g.repo, "repo", "", "path of the git repository (default: the repository of the current directory)")
	pf.BoolVar(&g.json, "json", false, "write data as JSON")
	pf.BoolVar(&g.noColor, "no-color", false, "do not use color")
	pf.BoolVarP(&g.verbose, "verbose", "v", false, "show debug output and full error detail")

	root.AddCommand(newVersionCmd())
	for _, add := range commandSet {
		root.AddCommand(add())
	}
	return root
}

var commandSet []func() *cobra.Command

func register(f func() *cobra.Command) { commandSet = append(commandSet, f) }

func Execute(args []string) int {
	g = globals{}
	xlog.Init(config.DefaultPaths().LogFile, false)
	root := NewRoot()
	root.SetArgs(args)
	err := root.Execute()
	if err == nil {
		return 0
	}
	var ec exitCode
	if errors.As(err, &ec) {
		return int(ec)
	}
	code := 1
	msg := err.Error()
	hint := ""
	var xe *Error
	if errors.As(err, &xe) {
		code = xe.Code
		hint = xe.Hint
	} else if strings.Contains(msg, "unknown command") || strings.Contains(msg, "flag") || strings.Contains(msg, "arg") {
		hint = "Run `xis --help` for usage."
	}
	fmt.Fprintf(os.Stderr, "xis: %s\n", firstLine(msg))
	if hint != "" {
		fmt.Fprintf(os.Stderr, "hint: %s\n", hint)
	}
	if g.verbose && xe != nil && xe.Err != nil {
		fmt.Fprintf(os.Stderr, "detail: %+v\n", xe.Err)
	}
	slog.Error("command failed", "err", firstLine(msg))
	return code
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}
