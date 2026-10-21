package rules

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRelPathThroughSymlink(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "private", "repo")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(filepath.Join(base, "private"), link); err != nil {
		t.Fatal(err)
	}
	top, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	c := &Context{RepoTop: top}
	for in, want := range map[string]string{
		filepath.Join(top, "a.go"):               "a.go",
		filepath.Join(link, "repo", "a.go"):      "a.go",
		filepath.Join(link, "repo", "gone/b.go"): "gone/b.go",
		"./c.go":                                 "c.go",
	} {
		if got := c.RelPath(in); got != want {
			t.Errorf("RelPath(%q) = %q, want %q", in, got, want)
		}
	}
}
