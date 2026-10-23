package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestDefaultTOMLMatchesDefault(t *testing.T) {
	var c Config
	if _, err := toml.Decode(DefaultTOML, &c); err != nil {
		t.Fatalf("decode default toml: %v", err)
	}
	d := Default()
	for _, k := range Keys() {
		got, _ := c.Get(k)
		want, _ := d.Get(k)
		if got != want {
			t.Errorf("%s: DefaultTOML has %q, Default() has %q", k, got, want)
		}
	}
}

func TestLoadPrecedence(t *testing.T) {
	dir := t.TempDir()
	p := PathsAt(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := WriteFile(p.ConfigFile, []byte("[llm]\nmodel = \"user-model\"\nprovider = \"none\"\n[rules]\nlarge_change_lines = 10\n")); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".xis.toml"), []byte("[rules]\nlarge_change_lines = 20\n[scope]\npaths = [\"src/\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XIS_LLM_MODEL", "env-model")
	t.Setenv("XIS_RULES_DISABLED", "A,B")
	c, err := Load(p, repo)
	if err != nil {
		t.Fatal(err)
	}
	if c.LLM.Model != "env-model" {
		t.Errorf("model = %q, want env-model", c.LLM.Model)
	}
	if c.Rules.LargeChangeLines != 20 {
		t.Errorf("large_change_lines = %d, want 20", c.Rules.LargeChangeLines)
	}
	if len(c.Rules.Disabled) != 2 || c.Rules.Disabled[1] != "B" {
		t.Errorf("disabled = %v", c.Rules.Disabled)
	}
	if len(c.Scope.Paths) != 1 {
		t.Errorf("scope.paths = %v", c.Scope.Paths)
	}
	if c.LLM.EffectiveProvider() != "none" {
		t.Errorf("provider = %q", c.LLM.EffectiveProvider())
	}
}

func TestEffectiveProviderAuto(t *testing.T) {
	l := Default().LLM
	t.Setenv("ANTHROPIC_API_KEY", "")
	if got := l.EffectiveProvider(); got != "none" {
		t.Errorf("no key: got %q", got)
	}
	t.Setenv("ANTHROPIC_API_KEY", "x")
	if got := l.EffectiveProvider(); got != "anthropic" {
		t.Errorf("key: got %q", got)
	}
}

func TestSetInFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c", "config.toml")
	if err := SetInFile(path, "llm.model", "m1"); err != nil {
		t.Fatal(err)
	}
	if err := SetInFile(path, "rules.disabled", "X,Y"); err != nil {
		t.Fatal(err)
	}
	if err := SetInFile(path, "rules.large_change_files", "7"); err != nil {
		t.Fatal(err)
	}
	if err := SetInFile(path, "nope.key", "1"); err == nil {
		t.Error("want error for unknown key")
	}
	if err := SetInFile(path, "rules.large_change_files", "abc"); err == nil {
		t.Error("want error for bad int")
	}
	c := Default()
	if _, err := toml.DecodeFile(path, &c); err != nil {
		t.Fatal(err)
	}
	if c.LLM.Model != "m1" || len(c.Rules.Disabled) != 2 || c.Rules.LargeChangeFiles != 7 {
		t.Errorf("got %+v %+v", c.LLM, c.Rules)
	}
	fi, _ := os.Stat(path)
	if runtime.GOOS != "windows" && fi.Mode().Perm() != FileMode {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
}

func TestEnsurePermissions(t *testing.T) {
	dir := t.TempDir()
	p := PathsAt(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{p.ConfigDir, p.DataDir, p.Spool, p.Shadow, p.Keys, p.Logs, p.LLMCache} {
		fi, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && fi.Mode().Perm() != DirMode {
			t.Errorf("%s mode = %v", d, fi.Mode().Perm())
		}
	}
}

func TestDefaultPathsXDG(t *testing.T) {
	c, d := filepath.Join(t.TempDir(), "c"), filepath.Join(t.TempDir(), "d")
	t.Setenv("XDG_CONFIG_HOME", c)
	t.Setenv("XDG_DATA_HOME", d)
	p := DefaultPaths()
	if p.ConfigFile != filepath.Join(c, "xis", "config.toml") || p.DB != filepath.Join(d, "xis", "xis.db") {
		t.Errorf("got %+v", p)
	}
}

func TestDefaultPathsWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows folders")
	}
	roaming, local := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("APPDATA", roaming)
	t.Setenv("LOCALAPPDATA", local)
	p := DefaultPaths()
	if p.ConfigDir != filepath.Join(roaming, "xis") || p.DataDir != filepath.Join(local, "xis") {
		t.Errorf("got %+v", p)
	}
}

func TestValidate(t *testing.T) {
	c := Default()
	c.LLM.Provider = "bogus"
	if c.Validate() == nil {
		t.Error("want error")
	}
	c = Default()
	c.Rules.Severity = map[string]string{"X": "huge"}
	if c.Validate() == nil {
		t.Error("want error")
	}
}
