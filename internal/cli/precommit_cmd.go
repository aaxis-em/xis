package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aaxis-em/xis/internal/gitx"
)

func init() { register(newPrecommitCmd) }

const gitHookMarker = "# xis-managed hook"

func gitHookScript(name, body string) string {
	return fmt.Sprintf(`#!/bin/sh
%s: runs the original %s hook (if any), then xis.
hook_dir=$(dirname "$0")
if [ -x "$hook_dir/%s.xis-orig" ]; then
  "$hook_dir/%s.xis-orig" "$@" || exit $?
fi
%s
`, gitHookMarker, name, name, name, body)
}

func preCommitBody(xis string) string {
	return xis + ` flags --staged --fail-on high
rc=$?
if [ "$rc" -eq 1 ]; then
  echo "xis: high-severity flags block this commit. Run 'xis flags --staged' to see them, or use 'git commit --no-verify'." >&2
  exit 1
fi
exit 0`
}

func postCommitBody(xis string) string {
	return xis + ` note write --quiet >/dev/null 2>&1 || true`
}

func gitHooksDir(repo string) (string, error) {
	return gitx.In(repo).Run("rev-parse", "--path-format=absolute", "--git-path", "hooks")
}

// installGitHook keeps an existing hook: it renames it to NAME.xis-orig, and
// the xis hook runs it first.
func installGitHook(dir, name, body string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	orig := path + ".xis-orig"
	script := gitHookScript(name, body)
	b, err := os.ReadFile(path)
	switch {
	case err == nil && strings.Contains(string(b), gitHookMarker):
		if string(b) == script {
			return fmt.Sprintf("%s: no change", path), nil
		}
	case err == nil:
		if _, err := os.Stat(orig); err == nil {
			return "", fmt.Errorf("%s and %s both exist; move one of them away", path, orig)
		}
		if err := os.Rename(path, orig); err != nil {
			return "", err
		}
	case !os.IsNotExist(err):
		return "", err
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(orig); err == nil {
		return fmt.Sprintf("%s: installed (runs the existing hook %s first)", path, filepath.Base(orig)), nil
	}
	return fmt.Sprintf("%s: installed", path), nil
}

func uninstallGitHook(dir, name string) (string, error) {
	path := filepath.Join(dir, name)
	orig := path + ".xis-orig"
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return fmt.Sprintf("%s: not installed", path), nil
	}
	if err != nil {
		return "", err
	}
	if !strings.Contains(string(b), gitHookMarker) {
		return fmt.Sprintf("%s: not an xis hook, kept", path), nil
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	if _, err := os.Stat(orig); err == nil {
		if err := os.Rename(orig, path); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s: removed, original hook restored", path), nil
	}
	return fmt.Sprintf("%s: removed", path), nil
}

func newPrecommitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "precommit",
		Short: "Install or remove the git pre-commit hook that runs `xis flags --staged --fail-on high`",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Install the pre-commit hook (and the post-commit hook if notes.auto is true)",
		Long:  "Install the pre-commit hook. An existing hook stays: xis renames it to pre-commit.xis-orig and runs it first.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := newApp(cmd)
			if err := a.requireRepo(); err != nil {
				return err
			}
			dir, err := gitHooksDir(a.RepoDir)
			if err != nil {
				return userErr(err.Error(), "Run `xis doctor`.", err)
			}
			xis := selfCommand()
			msg, err := installGitHook(dir, "pre-commit", preCommitBody(xis))
			if err != nil {
				return userErr(err.Error(), "", err)
			}
			a.printf("%s\n", msg)
			if a.Cfg.Notes.Auto {
				msg, err := installGitHook(dir, "post-commit", postCommitBody(xis))
				if err != nil {
					return userErr(err.Error(), "", err)
				}
				a.printf("%s\n", msg)
			}
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "uninstall",
		Short: "Remove the xis pre-commit and post-commit hooks and restore the original hooks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a := newApp(cmd)
			if err := a.requireRepo(); err != nil {
				return err
			}
			dir, err := gitHooksDir(a.RepoDir)
			if err != nil {
				return userErr(err.Error(), "", err)
			}
			for _, name := range []string{"pre-commit", "post-commit"} {
				msg, err := uninstallGitHook(dir, name)
				if err != nil {
					return userErr(err.Error(), "", err)
				}
				a.printf("%s\n", msg)
			}
			return nil
		},
	})
	return cmd
}
