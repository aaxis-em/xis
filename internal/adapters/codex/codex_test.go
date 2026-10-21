package codex

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/aaxis-em/xis/internal/model"
)

var update = flag.Bool("update", false, "rewrite golden files")

const fixture = "../../../testdata/fixtures/codex/rollout-2026-10-20T09-00-00-019a2b3c-4d5e-7f60-8a9b-0c1d2e3f4a5b.jsonl"

func TestParseGolden(t *testing.T) {
	res, err := Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	path := filepath.Join("testdata", "rollout.golden.json")
	if *update {
		_ = os.MkdirAll("testdata", 0o755)
		_ = os.WriteFile(path, append(b, '\n'), 0o644)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if diff := cmp.Diff(strings.TrimSpace(string(want)), strings.TrimSpace(string(b))); diff != "" {
		t.Errorf("golden (-want +got):\n%s", diff)
	}

	tr := res.Trace
	if tr.Session.Fidelity != model.FidelityImported || tr.Session.AgentSessionID != "019a2b3c-4d5e-7f60-8a9b-0c1d2e3f4a5b" ||
		tr.Session.Models[0] != "gpt-5-codex" || tr.Session.AgentVersion != "0.47.0" || res.Cwd != "/work/calc" {
		t.Errorf("session = %+v", tr.Session)
	}
	kinds := map[model.StepKind]int{}
	for _, s := range tr.Steps {
		kinds[s.Kind]++
	}
	if kinds[model.StepUserMessage] != 1 || kinds[model.StepReasoning] != 1 || kinds[model.StepToolCall] != 4 || kinds[model.StepAssistantMessage] != 2 {
		t.Errorf("kinds = %v", kinds)
	}
	if len(tr.Edits) != 1 || tr.Edits[0].FilePath != "calc.go" || tr.Edits[0].Added != 4 || tr.Edits[0].Removed != 2 {
		t.Errorf("edits = %+v", tr.Edits)
	}
	for _, tc := range tr.ToolCalls {
		if tc.ToolName == "shell" && (tc.ExitCode == nil || *tc.ExitCode != 0) {
			t.Errorf("%s exit = %v", tc.Input, tc.ExitCode)
		}
	}
	if len(tr.Warnings) != 1 || !strings.Contains(tr.Warnings[0], "future_record_type") {
		t.Errorf("warnings = %v", tr.Warnings)
	}
}

func TestOutputShapes(t *testing.T) {
	cases := map[string]int{
		`"{\"output\":\"x\",\"metadata\":{\"exit_code\":2}}"`: 2,
		`"Exit code: 1\nWall time: 1s\nOutput:\nFAIL"`:        1,
		`"Process exited with code 3"`:                        3,
	}
	for raw, want := range cases {
		_, code := outputText(json.RawMessage(raw))
		if code == nil || *code != want {
			t.Errorf("%s: code %v want %d", raw, code, want)
		}
	}
	if _, code := outputText(json.RawMessage(`[{"type":"input_text","text":"hi"}]`)); code != nil {
		t.Error("content items have no exit code")
	}
}

func TestCommandAndPatch(t *testing.T) {
	if c := Command("shell", `{"command":["bash","-lc","go test ./..."]}`); c != "go test ./..." {
		t.Errorf("command %q", c)
	}
	if c := Command("exec_command", `{"cmd":"ls -la"}`); c != "ls -la" {
		t.Errorf("command %q", c)
	}
	p := "*** Begin Patch\n*** Add File: a.go\n+package a\n*** Update File: b.go\n*** Move to: c.go\n@@\n-x\n+y\n*** Delete File: d.go\n*** End Patch"
	fes := ParsePatch(p)
	if len(fes) != 3 || !fes[0].Created || fes[1].Path != "c.go" || fes[1].OldPath != "b.go" || !fes[2].Deleted {
		t.Errorf("patch %+v", fes)
	}
	if PatchOf("shell", `{"command":["apply_patch","*** Begin Patch\n*** Add File: a.go\n+x\n*** End Patch"]}`) == "" {
		t.Error("apply_patch through shell not found")
	}
}
