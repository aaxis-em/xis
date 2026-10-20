package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/aaxis-em/xis/internal/attribution"
	"github.com/aaxis-em/xis/internal/config"
	"github.com/aaxis-em/xis/internal/model"
	"github.com/aaxis-em/xis/internal/store"
)

type scenario struct {
	t          *testing.T
	repo       string
	session    string
	transcript string
}

func newScenario(t *testing.T) *scenario {
	t.Helper()
	isolate(t)
	t.Setenv("XIS_NO_ASYNC_INGEST", "1")
	repo := filepath.Join(t.TempDir(), "calc repo")
	out, err := exec.Command("sh", "../../testdata/repos/calc.sh", repo).CombinedOutput()
	if err != nil {
		t.Fatalf("calc.sh: %v\n%s", err, out)
	}
	s := &scenario{t: t, repo: repo, session: "e2e-session-1", transcript: filepath.Join(t.TempDir(), "transcript.jsonl")}
	return s
}

func (s *scenario) write(name, content string) {
	s.t.Helper()
	p := filepath.Join(s.repo, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		s.t.Fatal(err)
	}
}

func (s *scenario) remove(name string) {
	s.t.Helper()
	if err := os.Remove(filepath.Join(s.repo, name)); err != nil {
		s.t.Fatal(err)
	}
}

func (s *scenario) transcriptLine(v map[string]any) {
	s.t.Helper()
	v["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	v["sessionId"] = s.session
	v["version"] = "2.1.304"
	v["cwd"] = s.repo
	b, _ := json.Marshal(v)
	f, err := os.OpenFile(s.transcript, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		s.t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		s.t.Fatal(err)
	}
}

func (s *scenario) hook(event string, fields map[string]any) {
	s.t.Helper()
	p := map[string]any{"session_id": s.session, "cwd": s.repo, "hook_event_name": event, "transcript_path": s.transcript}
	for k, v := range fields {
		p[k] = v
	}
	b, _ := json.Marshal(p)
	hookStdin = strings.NewReader(string(b))
	out, err := run(s.t, "hook", "claude-code", event)
	if err != nil || out != "" {
		s.t.Fatalf("hook %s: out=%q err=%v", event, out, err)
	}
	time.Sleep(2 * time.Millisecond)
}

func (s *scenario) prompt(text string) {
	s.hook("UserPromptSubmit", map[string]any{"prompt": text})
	s.transcriptLine(map[string]any{"type": "user", "uuid": fmt.Sprintf("u-%d", time.Now().UnixNano()), "origin": map[string]any{"kind": "human"},
		"message": map[string]any{"role": "user", "content": text}})
}

func (s *scenario) say(text string) {
	s.transcriptLine(map[string]any{"type": "assistant", "uuid": fmt.Sprintf("a-%d", time.Now().UnixNano()),
		"message": map[string]any{"id": fmt.Sprintf("msg-%d", time.Now().UnixNano()), "role": "assistant", "model": "claude-test-1",
			"content": []any{map[string]any{"type": "text", "text": text}}}})
}

func (s *scenario) tool(id, name string, input map[string]any, change func(), output string, exit int) {
	s.transcriptLine(map[string]any{"type": "assistant", "uuid": "a-" + id,
		"message": map[string]any{"id": "msg-" + id, "role": "assistant", "model": "claude-test-1",
			"content": []any{map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}}}})
	s.hook("PreToolUse", map[string]any{"tool_name": name, "tool_use_id": id, "tool_input": input})
	if change != nil {
		change()
	}
	if exit == 0 {
		s.hook("PostToolUse", map[string]any{"tool_name": name, "tool_use_id": id, "tool_input": input,
			"tool_response": map[string]any{"stdout": output, "stderr": "", "interrupted": false}})
	} else {
		s.hook("PostToolUseFailure", map[string]any{"tool_name": name, "tool_use_id": id, "tool_input": input,
			"error": fmt.Sprintf("Exit code %d\n%s", exit, output)})
	}
	isErr := exit != 0
	content := output
	if isErr {
		content = fmt.Sprintf("Exit code %d\n%s", exit, output)
	}
	s.transcriptLine(map[string]any{"type": "user", "uuid": "r-" + id,
		"message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": content, "is_error": isErr}}}})
}

const calcFixed = "package calc\n\n// Add returns the sum of a and b.\nfunc Add(a, b int) int {\n\treturn a + b\n}\n"

// runCalcScenario drives the hooks through a session with a Bash edit, a
// reverted file, a reverted line, a failing test run, a test edit after the
// failure, and a human edit between turns.
func runCalcScenario(s *scenario) {
	s.hook("SessionStart", map[string]any{"source": "startup"})
	s.prompt("Fix the Add function in calc.go and make the tests pass.")
	s.say("The bug is in Add: it subtracts. I will make it add.")
	s.tool("t1-read", "Read", map[string]any{"file_path": filepath.Join(s.repo, "calc.go")}, nil, "", 0)
	s.tool("t2-edit", "Edit", map[string]any{"file_path": filepath.Join(s.repo, "calc.go"), "old_string": "a - b", "new_string": "a + b"},
		func() { s.write("calc.go", calcFixed) }, "", 0)
	s.tool("t3-bash", "Bash", map[string]any{"command": "sed -i 's/1.0/1.1/' util.go"},
		func() { s.write("util.go", "package calc\n\nconst Version = \"1.1\"\n") }, "", 0)
	s.tool("t4-write", "Write", map[string]any{"file_path": filepath.Join(s.repo, "tmp.go"), "content": "package calc\n"},
		func() { s.write("tmp.go", "package calc\n\nvar scratch = 1\n") }, "", 0)
	s.tool("t5-rm", "Bash", map[string]any{"command": "rm tmp.go"}, func() { s.remove("tmp.go") }, "", 0)
	s.tool("t6-debug", "Edit", map[string]any{"file_path": filepath.Join(s.repo, "calc.go")},
		func() {
			s.write("calc.go", strings.Replace(calcFixed, "\treturn a + b\n", "\tprintln(\"debug\")\n\treturn a + b\n", 1))
		}, "", 0)
	s.tool("t7-undebug", "Edit", map[string]any{"file_path": filepath.Join(s.repo, "calc.go")},
		func() { s.write("calc.go", calcFixed) }, "", 0)
	s.tool("t8-test", "Bash", map[string]any{"command": "go test ./..."}, nil, "--- FAIL: TestAdd (0.00s)\nFAIL\nFAIL\tcalc\t0.002s", 1)
	s.say("The test expects the old value. I will update the test.")
	s.tool("t9-testedit", "Edit", map[string]any{"file_path": filepath.Join(s.repo, "calc_test.go")},
		func() {
			s.write("calc_test.go", "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(2, 3) != 5 {\n\t\tt.Fatal(\"bad\")\n\t}\n\tif Add(1, 1) != 2 {\n\t\tt.Fatal(\"bad\")\n\t}\n}\n")
		}, "", 0)
	s.tool("t10-test", "Bash", map[string]any{"command": "go test ./..."}, nil, "ok  \tcalc\t0.002s", 0)
	s.say("All tests pass now.")
	s.hook("Stop", map[string]any{"last_assistant_message": "All tests pass now."})
	s.write("README.md", "# calc\n\nA tiny calculator.\n")
	s.prompt("Thanks. Also bump nothing else.")
	s.say("Done.")
	s.hook("Stop", map[string]any{"last_assistant_message": "Done."})
	s.hook("SessionEnd", map[string]any{"reason": "other"})
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(config.DefaultPaths().DB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func provenanceByFile(t *testing.T, st *store.Store, res *attribution.Result) map[string][]string {
	t.Helper()
	d, err := st.LoadSession(res.Sessions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	label := func(stepID string) string {
		if stepID == model.Unattributed {
			return "unattributed"
		}
		if tc, ok := d.ToolCalls[stepID]; ok {
			return tc.ToolUseID
		}
		s, _ := d.Step(stepID)
		return string(s.Kind)
	}
	hunkFile := map[string]string{}
	for _, h := range res.Hunks {
		hunkFile[h.ID] = h.FilePath
	}
	out := map[string][]string{}
	for _, p := range res.Provenance {
		f := hunkFile[p.HunkID]
		out[f] = append(out[f], fmt.Sprintf("%s:%d", label(p.StepID), p.Lines))
	}
	for f := range out {
		sort.Strings(out[f])
	}
	return out
}

func TestE2EAttribution(t *testing.T) {
	s := newScenario(t)
	runCalcScenario(s)

	out, err := run(t, "ingest", "--session", s.session)
	if err != nil {
		t.Fatalf("ingest: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(config.DefaultPaths().Spool, s.session+".jsonl")); !os.IsNotExist(err) {
		t.Error("spool file not deleted after ingest")
	}

	g.repo = s.repo
	st := openTestStore(t)
	app := &App{Paths: config.DefaultPaths(), Cfg: config.Default(), RepoDir: s.repo}
	_, res, err := app.computeChange(st, attribution.Options{Mode: attribution.ModeSession})
	if err != nil {
		t.Fatal(err)
	}
	got := provenanceByFile(t, st, res)
	t.Logf("provenance: %v", got)
	want := map[string][]string{
		"calc.go":      {"t2-edit:3"},
		"util.go":      {"t3-bash:2"},
		"calc_test.go": {"t9-testedit:3"},
		"README.md":    {"human_edit:2"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("provenance (-want +got):\n%s", diff)
	}

	d, err := st.LoadSession(res.Sessions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	var humanEdits int
	for _, step := range d.Steps {
		if step.Kind == model.StepHumanEdit {
			humanEdits++
		}
	}
	if humanEdits != 1 {
		t.Errorf("human edit steps = %d, want 1", humanEdits)
	}
	var outcomes []string
	for _, r := range d.TestRuns {
		outcomes = append(outcomes, r.Outcome)
	}
	if diff := cmp.Diff([]string{"fail", "pass"}, outcomes); diff != "" {
		t.Errorf("test runs (-want +got):\n%s", diff)
	}
	reverted := 0
	for _, e := range d.Edits {
		if e.RevertedByEditID != "" {
			reverted++
		}
	}
	if reverted < 2 {
		t.Errorf("reverted edits = %d, want >= 2 (tmp.go and the debug line)", reverted)
	}

	tl := buildTimeline(st, d, res)
	abandoned := map[string]bool{}
	for _, step := range tl.Steps {
		if step.Abandoned {
			tc := d.ToolCalls[step.ID]
			abandoned[tc.ToolUseID] = true
		}
	}
	if diff := cmp.Diff(map[string]bool{"t4-write": true, "t5-rm": true, "t6-debug": true, "t7-undebug": true}, abandoned); diff != "" {
		t.Errorf("abandoned (-want +got):\n%s", diff)
	}

	whyOut, err := run(t, "--repo", s.repo, "why", filepath.Join(s.repo, "util.go")+":3")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"sed -i", "Fix the Add function"} {
		if !strings.Contains(whyOut, w) {
			t.Errorf("why output misses %q:\n%s", w, whyOut)
		}
	}
	whyOut, _ = run(t, "--repo", s.repo, "why", filepath.Join(s.repo, "calc.go")+":3")
	if !strings.Contains(whyOut, "The bug is in Add") {
		t.Errorf("why misses agent text:\n%s", whyOut)
	}

	out2, err := run(t, "ingest", "--session", s.session)
	if err != nil || !strings.Contains(out2, "nothing") && !strings.Contains(out2, "ingested") {
		t.Errorf("second ingest: %v %s", err, out2)
	}
}
