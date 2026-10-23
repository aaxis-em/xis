package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/aaxis-em/xis/internal/textx"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const DefaultTimeout = 60 * time.Second

type Error struct {
	Args     []string
	Stderr   string
	ExitCode int
	Err      error
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" && e.Err != nil {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), textx.FirstLine(msg))
}

func (e *Error) Unwrap() error { return e.Err }

// Variables that git sets for hooks would redirect our commands to the wrong repo or index.
var inheritedGitVars = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_PREFIX",
}

type Git struct {
	Dir     string
	Env     []string
	Timeout time.Duration
	Stdin   io.Reader
}

func In(dir string) Git { return Git{Dir: dir} }

func (g Git) With(env ...string) Git {
	g.Env = append(append([]string{}, g.Env...), env...)
	return g
}

func (g Git) WithStdin(r io.Reader) Git {
	g.Stdin = r
	return g
}

func (g Git) environ() []string {
	drop := map[string]bool{}
	for _, k := range inheritedGitVars {
		drop[k] = true
	}
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[k] {
			env = append(env, kv)
		}
	}
	env = append(env, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1")
	return append(env, g.Env...)
}

func (g Git) Output(args ...string) ([]byte, error) {
	timeout := g.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.Dir
	cmd.Env = g.environ()
	cmd.Stdin = g.Stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		ge := &Error{Args: args, Stderr: stderr.String(), Err: err, ExitCode: -1}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			ge.ExitCode = ee.ExitCode()
		}
		if ctx.Err() != nil {
			ge.Err = fmt.Errorf("timeout after %s: %w", timeout, ctx.Err())
		}
		return stdout.Bytes(), ge
	}
	return stdout.Bytes(), nil
}

func (g Git) Run(args ...string) (string, error) {
	out, err := g.Output(args...)
	return strings.TrimRight(string(out), "\n"), err
}

func ExitCode(err error) int {
	var ge *Error
	if errors.As(err, &ge) {
		return ge.ExitCode
	}
	return -1
}

func Version() (string, error) {
	out, err := Git{}.Run("version")
	return strings.TrimPrefix(out, "git version "), err
}
