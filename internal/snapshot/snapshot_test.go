package snapshot

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "-c", "gc.auto=0", "-c", "maintenance.auto=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newRepo(t testing.TB) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "my repo ü")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	write(t, filepath.Join(dir, "a.go"), "package a\n")
	write(t, filepath.Join(dir, ".gitignore"), "ignored.txt\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	return dir
}

func fingerprint(t testing.TB, repo string) string {
	t.Helper()
	h := sha256.New()
	for _, f := range []string{"index", "HEAD", "packed-refs"} {
		b, _ := os.ReadFile(filepath.Join(repo, ".git", f))
		h.Write(b)
	}
	refs := git(t, repo, "for-each-ref")
	h.Write([]byte(refs))
	objs, _ := filepath.Glob(filepath.Join(repo, ".git", "objects", "*", "*"))
	fmt.Fprint(h, len(objs))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func TestTakeChain(t *testing.T) {
	repo := newRepo(t)
	head := git(t, repo, "rev-parse", "HEAD")
	before := fingerprint(t, repo)

	r, err := Discover(filepath.Join(repo))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.ID) != 16 {
		t.Errorf("repo id %q", r.ID)
	}
	sh, err := Open(filepath.Join(t.TempDir(), "shadow"), r, []string{".env*", "node_modules/"})
	if err != nil {
		t.Fatal(err)
	}

	c0, err := sh.Take("sess-1", "SessionStart")
	if err != nil {
		t.Fatal(err)
	}
	if p := git(t, repo, "--git-dir", sh.Path, "rev-parse", c0+"^"); p != head {
		t.Errorf("root parent = %s, want HEAD %s", p, head)
	}

	write(t, filepath.Join(repo, "a.go"), "package a\n\nfunc A() {}\n")
	write(t, filepath.Join(repo, "new file.txt"), "untracked\n")
	write(t, filepath.Join(repo, "ignored.txt"), "x\n")
	write(t, filepath.Join(repo, ".env.local"), "SECRET=1\n")
	write(t, filepath.Join(repo, "web", "node_modules", "m.js"), "x\n")

	c1, err := sh.Take("sess-1", "toolu_1")
	if err != nil {
		t.Fatal(err)
	}
	if p := git(t, repo, "--git-dir", sh.Path, "rev-parse", c1+"^"); p != c0 {
		t.Errorf("parent = %s, want %s", p, c0)
	}
	if h := sh.Head("sess-1"); h != c1 {
		t.Errorf("head = %s, want %s", h, c1)
	}
	files := git(t, repo, "--git-dir", sh.Path, "ls-tree", "-r", "--name-only", c1)
	for _, want := range []string{"a.go", "new file.txt"} {
		if !strings.Contains(files, want) {
			t.Errorf("missing %s in snapshot: %s", want, files)
		}
	}
	for _, bad := range []string{"ignored.txt", ".env.local", "node_modules"} {
		if strings.Contains(files, bad) {
			t.Errorf("snapshot contains %s: %s", bad, files)
		}
	}
	diff := git(t, repo, "--git-dir", sh.Path, "diff", "--name-only", c0, c1)
	if !strings.Contains(diff, "a.go") {
		t.Errorf("diff = %s", diff)
	}
	if after := fingerprint(t, repo); after != before {
		t.Error("snapshot changed the user's repository")
	}
	if st := git(t, repo, "status", "--porcelain"); !strings.Contains(st, "?? \"new file.txt\"") && !strings.Contains(st, "?? new file.txt") {
		t.Errorf("user index changed: %s", st)
	}
	ids, err := sh.Sessions()
	if err != nil || len(ids) != 1 || ids[0] != "sess-1" {
		t.Errorf("sessions = %v, %v", ids, err)
	}
	if err := sh.DeleteSession("sess-1"); err != nil {
		t.Fatal(err)
	}
	if err := sh.GC(); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverNotGit(t *testing.T) {
	if _, err := Discover(t.TempDir()); !errors.Is(err, ErrNotGit) {
		t.Errorf("err = %v", err)
	}
}

func TestEmptyRepoRoot(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	write(t, filepath.Join(dir, "x.txt"), "x\n")
	r, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	sh, err := Open(filepath.Join(t.TempDir(), "shadow"), r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sh.Take("s", "SessionStart"); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkTake(b *testing.B) {
	repo := filepath.Join(b.TempDir(), "r")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		b.Fatal(err)
	}
	git(b, repo, "init", "-q")
	for i := 0; i < 10000; i++ {
		write(b, filepath.Join(repo, fmt.Sprintf("d%02d", i%100), fmt.Sprintf("f%05d.txt", i)), fmt.Sprintf("line %d\n", i))
	}
	git(b, repo, "add", "-A")
	git(b, repo, "commit", "-qm", "init")
	r, err := Discover(repo)
	if err != nil {
		b.Fatal(err)
	}
	sh, err := Open(filepath.Join(b.TempDir(), "shadow"), r, nil)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := sh.Take("s", "warm"); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		write(b, filepath.Join(repo, "d00", "f00000.txt"), fmt.Sprintf("v%d\n", i))
		if _, err := sh.Take("s", "step"); err != nil {
			b.Fatal(err)
		}
	}
}
