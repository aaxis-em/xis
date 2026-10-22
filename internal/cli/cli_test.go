package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	g = globals{}
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir+"/config")
	t.Setenv("XDG_DATA_HOME", dir+"/data")
	t.Setenv("ANTHROPIC_API_KEY", "")
	return dir
}

func TestVersion(t *testing.T) {
	isolate(t)
	out, err := run(t, "version")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"xis ", "commit:", "built:"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in %q", w, out)
		}
	}
}

func TestConfigSetGet(t *testing.T) {
	isolate(t)
	if _, err := run(t, "config", "set", "llm.model", "abc"); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, "config", "get", "llm.model")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "abc" {
		t.Errorf("got %q", out)
	}
	if _, err := run(t, "config", "get", "no.such"); err == nil {
		t.Error("want error")
	}
}

func TestHookCommandNeverWritesStdout(t *testing.T) {
	isolate(t)
	for _, in := range []string{"{bad", "", `{"session_id":"x","cwd":"/nonexistent","hook_event_name":"PostToolUse","tool_name":"Edit"}`} {
		hookStdin = strings.NewReader(in)
		out, err := run(t, "hook", "claude-code", "PostToolUse")
		if err != nil || out != "" {
			t.Errorf("in=%q out=%q err=%v", in, out, err)
		}
	}
	hookStdin = strings.NewReader("{}")
	if out, err := run(t, "hook", "claude-code"); err != nil || out != "" {
		t.Errorf("no event: out=%q err=%v", out, err)
	}
	if out, err := run(t, "hook", "claude-code", "X", "--weird-flag"); err != nil || out != "" {
		t.Errorf("odd flag: out=%q err=%v", out, err)
	}
}

func TestDoctorRuns(t *testing.T) {
	isolate(t)
	if _, err := run(t, "init"); err != nil {
		t.Fatal(err)
	}
	out, _ := run(t, "doctor")
	for _, w := range []string{"git", "database", "hooks", "spool", "llm"} {
		if !strings.Contains(out, w) {
			t.Errorf("doctor output misses %s:\n%s", w, out)
		}
	}
}

func TestHookInstallRefusesTheHomeRepository(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	if out, err := exec.Command("git", "-C", home, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	sub := filepath.Join(home, "test")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	out, err := run(t, "hook", "install", "--agent", "claude-code", "--repo", sub)
	if err == nil || !strings.Contains(err.Error()+out, "home directory") {
		t.Errorf("install in the home repository: %v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Error("settings were written")
	}
}
