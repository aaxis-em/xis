package cli

import (
	"bytes"
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
