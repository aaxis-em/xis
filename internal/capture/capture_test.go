package capture

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aaxis-em/xis/internal/adapters/claudecode"
	"github.com/aaxis-em/xis/internal/adapters/codex"
	"github.com/aaxis-em/xis/internal/config"
)

func git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "-c", "gc.auto=0"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newHandler(t testing.TB) (*Handler, *[]string) {
	t.Helper()
	dir := t.TempDir()
	p := config.PathsAt(filepath.Join(dir, "cfg"), filepath.Join(dir, "data"))
	h := NewHandler(p, config.Default())
	var spawned []string
	var mu sync.Mutex
	h.Spawn = func(sid string) error {
		mu.Lock()
		defer mu.Unlock()
		spawned = append(spawned, sid)
		return nil
	}
	return h, &spawned
}

func newRepo(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "init")
	return dir
}

func payload(sid, cwd, event, tool, toolUseID string) string {
	m := map[string]any{"session_id": sid, "cwd": cwd, "hook_event_name": event, "transcript_path": "/nope.jsonl"}
	if tool != "" {
		m["tool_name"] = tool
		m["tool_use_id"] = toolUseID
		m["tool_input"] = map[string]any{"file_path": filepath.Join(cwd, "a.go")}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestHandlerSnapshotsAndSpool(t *testing.T) {
	h, spawned := newHandler(t)
	repo := newRepo(t)
	h.Handle("SessionStart", strings.NewReader(payload("s1", repo, "SessionStart", "", "")))
	h.Handle("PreToolUse", strings.NewReader(payload("s1", repo, "PreToolUse", "Read", "t1")))
	h.Handle("PostToolUse", strings.NewReader(payload("s1", repo, "PostToolUse", "Read", "t1")))
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.Handle("PostToolUse", strings.NewReader(payload("s1", repo, "PostToolUse", "Edit", "t2")))
	h.Handle("Stop", strings.NewReader(payload("s1", repo, "Stop", "", "")))

	lines, err := ReadSpool(SpoolPath(h.Paths.Spool, "s1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 5 {
		t.Fatalf("got %d lines", len(lines))
	}
	wantSnap := []bool{true, false, false, true, false}
	for i, l := range lines {
		if (l.Snapshot != nil) != wantSnap[i] {
			t.Errorf("line %d (%s): snapshot=%v", i, l.Event, l.Snapshot)
		}
		if l.V != 1 || l.Ts.IsZero() {
			t.Errorf("line %d: bad header %+v", i, l)
		}
	}
	if lines[0].Repo == nil || lines[0].Repo.Head == "" {
		t.Errorf("SessionStart line has no repo head: %+v", lines[0].Repo)
	}
	shadow := filepath.Join(h.Paths.Shadow, lines[0].Repo.ID+".git")
	if p := git(t, repo, "--git-dir", shadow, "rev-parse", *lines[3].Snapshot+"^"); p != *lines[0].Snapshot {
		t.Errorf("chain parent = %s, want %s", p, *lines[0].Snapshot)
	}
	if len(*spawned) != 1 || (*spawned)[0] != "s1" {
		t.Errorf("spawned = %v", *spawned)
	}
	fi, _ := os.Stat(SpoolPath(h.Paths.Spool, "s1"))
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("spool mode %v", fi.Mode().Perm())
	}
}

func TestHandlerSafety(t *testing.T) {
	cases := map[string]string{
		"malformed":      "{not json",
		"empty":          "",
		"missing fields": `{"hook_event_name":"PostToolUse"}`,
		"non-git cwd":    payload("s2", os.TempDir(), "PostToolUse", "Edit", "t"),
		"wrong types":    `{"session_id":5,"cwd":[1],"tool_name":{}}`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			h, _ := newHandler(t)
			h.Handle("PostToolUse", strings.NewReader(in))
		})
	}
	t.Run("unwritable data dir", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root ignores permissions")
		}
		h, _ := newHandler(t)
		if err := os.MkdirAll(h.Paths.DataDir, 0o500); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(h.Paths.DataDir, 0o700)
		h.Handle("SessionStart", strings.NewReader(payload("s3", newRepo(t), "SessionStart", "", "")))
	})
	t.Run("non-git marks no_git", func(t *testing.T) {
		h, _ := newHandler(t)
		h.Handle("UserPromptSubmit", strings.NewReader(payload("s4", t.TempDir(), "UserPromptSubmit", "", "")))
		lines, err := ReadSpool(SpoolPath(h.Paths.Spool, "s4"))
		if err != nil || len(lines) != 1 || !lines[0].NoGit || lines[0].Snapshot != nil {
			t.Fatalf("lines=%+v err=%v", lines, err)
		}
	})
	t.Run("malformed payload kept as string", func(t *testing.T) {
		h, _ := newHandler(t)
		h.Handle("Stop", strings.NewReader("{bad"))
		lines, err := ReadSpool(SpoolPath(h.Paths.Spool, "unknown"))
		if err != nil || len(lines) != 1 || string(lines[0].Payload) != `"{bad"` {
			t.Fatalf("lines=%+v err=%v", lines, err)
		}
	})
}

func TestConcurrentAppend(t *testing.T) {
	h, _ := newHandler(t)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h.Handle("PreToolUse", strings.NewReader(payload("c", "/", "PreToolUse", "Read", fmt.Sprint(i))))
		}(i)
	}
	wg.Wait()
	lines, err := ReadSpool(SpoolPath(h.Paths.Spool, "c"))
	if err != nil || len(lines) != 40 {
		t.Fatalf("lines=%d err=%v", len(lines), err)
	}
}

func TestInstallMergeIdempotentUninstall(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := `{
  "permissions": {"allow": ["Bash(ls)"]},
  "model": "opus",
  "hooks": {
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "my-check.sh", "timeout": 5}]}]
  }
}`
	if err := os.WriteFile(path, []byte(orig), 0o640); err != nil {
		t.Fatal(err)
	}
	changes, err := Install(path, "/opt/xis", claudecode.Agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 10 {
		t.Errorf("changes = %v", changes)
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	if strings.Index(s, `"permissions"`) > strings.Index(s, `"model"`) {
		t.Error("key order changed")
	}
	for _, want := range []string{"my-check.sh", `"/opt/xis hook claude-code SessionEnd"`, `"timeout": 1`, `"matcher": "*"`, `"Bash(ls)"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in\n%s", want, s)
		}
	}
	fi, _ := os.Stat(path)
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o640 {
		t.Errorf("mode changed to %v", fi.Mode().Perm())
	}
	again, err := Install(path, "/opt/xis", claudecode.Agent)
	if err != nil || len(again) != 0 {
		t.Errorf("second install changes=%v err=%v", again, err)
	}
	b2, _ := os.ReadFile(path)
	if string(b2) != s {
		t.Error("second install rewrote the file")
	}
	moved, err := Install(path, "/usr/bin/xis", claudecode.Agent)
	if err != nil || len(moved) != 10 || !strings.HasPrefix(moved[0], "updated") {
		t.Errorf("path change: %v %v", moved, err)
	}
	inst, _ := Installed(path)
	if len(inst) != 10 {
		t.Errorf("installed = %v", inst)
	}
	removed, err := Uninstall(path)
	if err != nil || len(removed) != 10 {
		t.Errorf("uninstall: %v %v", removed, err)
	}
	b3, _ := os.ReadFile(path)
	var m map[string]any
	if err := json.Unmarshal(b3, &m); err != nil {
		t.Fatal(err)
	}
	hooks := m["hooks"].(map[string]any)
	if len(hooks) != 1 || !strings.Contains(string(b3), "my-check.sh") {
		t.Errorf("uninstall left %s", b3)
	}
}

func TestInstallNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x", ".claude", "settings.json")
	if _, err := Install(path, "xis", claudecode.Agent); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, b)
	}
	if _, err := Uninstall(path); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if strings.TrimSpace(string(b)) != "{}" {
		t.Errorf("got %s", b)
	}
}

func BenchmarkHookNoSnapshot(b *testing.B) {
	h, _ := newHandler(b)
	in := payload("bench", "/", "PreToolUse", "Read", "t")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Handle("PreToolUse", strings.NewReader(in))
	}
}

func BenchmarkHookSnapshot(b *testing.B) {
	h, _ := newHandler(b)
	repo := newRepo(b)
	in := payload("bench", repo, "PostToolUse", "Edit", "t")
	h.Handle("SessionStart", strings.NewReader(payload("bench", repo, "SessionStart", "", "")))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = os.WriteFile(filepath.Join(repo, "a.go"), []byte(fmt.Sprintf("package a // %d\n", i)), 0o644)
		h.Handle("PostToolUse", strings.NewReader(in))
	}
}

func TestHooksDoNotWaitForIngest(t *testing.T) {
	h, _ := newHandler(t)
	repo := newRepo(t)
	spool := h.Paths.Spool
	h.Handle("UserPromptSubmit", strings.NewReader(payload("s1", repo, "UserPromptSubmit", "", "")))

	run, err := LockIngest(spool, "s1", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	lines, size, err := TakeSpool(spool, "s1")
	if err != nil || len(lines) != 1 {
		t.Fatalf("take: %d lines, %v", len(lines), err)
	}

	start := time.Now()
	h.Handle("PreToolUse", strings.NewReader(payload("s1", repo, "PreToolUse", "Edit", "t1")))
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("hook waited %s while an ingest ran", d)
	}

	if err := DropSpool(spool, "s1", size); err != nil {
		t.Fatal(err)
	}
	run.Release()
	rest, err := ReadSpool(SpoolPath(spool, "s1"))
	if err != nil || len(rest) != 1 || rest[0].Event != "PreToolUse" {
		t.Fatalf("after drop: %+v %v", rest, err)
	}

	_, size, _ = TakeSpool(spool, "s1")
	if err := DropSpool(spool, "s1", size); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(SpoolPath(spool, "s1")); !os.IsNotExist(err) {
		t.Error("spool not removed after the last drop")
	}
	if _, err := os.Stat(filepath.Join(spool, "s1.lock")); err != nil {
		t.Error("lock file removed with the spool")
	}
}

func TestNoSnapshotOfTheHomeRepository(t *testing.T) {
	h, _ := newHandler(t)
	repo := newRepo(t)
	t.Setenv("HOME", repo)
	t.Setenv("USERPROFILE", repo)
	sub := filepath.Join(repo, "test")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	h.Handle("UserPromptSubmit", strings.NewReader(payload("s1", sub, "UserPromptSubmit", "", "")))
	lines, err := ReadSpool(SpoolPath(h.Paths.Spool, "s1"))
	if err != nil || len(lines) != 1 {
		t.Fatalf("%d lines, %v", len(lines), err)
	}
	if lines[0].Snapshot != nil || !strings.Contains(lines[0].SnapshotError, "home directory") {
		t.Errorf("line = %+v", lines[0])
	}
}

func TestWindowsHookCommandIsRecognized(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude", "settings.json")
	cmd := `"C:/Users/Some One/AppData/Local/Programs/xis/xis.exe"`
	if _, err := Install(path, cmd, claudecode.Agent); err != nil {
		t.Fatal(err)
	}
	if again, err := Install(path, cmd, claudecode.Agent); err != nil || len(again) != 0 {
		t.Errorf("second install changed %v (%v)", again, err)
	}
	if got, err := Installed(path); err != nil || len(got) == 0 {
		t.Errorf("Windows hooks not found: %v %v", got, err)
	}
	if _, err := Uninstall(path); err != nil {
		t.Fatal(err)
	}
	if got, _ := Installed(path); len(got) != 0 {
		t.Errorf("hooks left after uninstall: %v", got)
	}
}

func TestCodexHooksInstall(t *testing.T) {
	repo := t.TempDir()
	path, err := SettingsPath(codex.Agent, "project", repo, "")
	if err != nil || path != filepath.Join(repo, ".codex", "hooks.json") {
		t.Fatalf("path = %s, %v", path, err)
	}
	if _, err := Install(path, "xis", codex.Agent); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"xis hook codex PostToolUse"`) || strings.Contains(string(b), "PostToolUseFailure") {
		t.Errorf("hooks.json:\n%s", b)
	}
	if got, _ := Installed(path); len(got) != len(codex.Events) {
		t.Errorf("installed %d events, want %d", len(got), len(codex.Events))
	}
	if _, err := Uninstall(path); err != nil {
		t.Fatal(err)
	}
	if got, _ := Installed(path); len(got) != 0 {
		t.Errorf("hooks left after uninstall: %v", got)
	}
}
