package ingest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aaxis-em/xis/internal/adapters/claudecode"
	"github.com/aaxis-em/xis/internal/model"
)

const codexRollout = "../../testdata/fixtures/codex/rollout-2026-10-20T09-00-00-019a2b3c-4d5e-7f60-8a9b-0c1d2e3f4a5b.jsonl"

func codexEvents(rollout string) []model.HookEvent {
	t0 := time.Date(2026, 10, 20, 9, 0, 0, 500e6, time.UTC)
	var out []model.HookEvent
	add := func(sec float64, event, snap string, extra map[string]any) {
		m := map[string]any{"session_id": "019a2b3c-4d5e-7f60-8a9b-0c1d2e3f4a5b", "cwd": "/work/calc", "hook_event_name": event, "transcript_path": rollout}
		for k, v := range extra {
			m[k] = v
		}
		b, _ := json.Marshal(m)
		out = append(out, model.HookEvent{Key: fmt.Sprint(len(out)), Seq: len(out), Ts: t0.Add(time.Duration(sec * float64(time.Second))), Event: event, Payload: string(b), Snapshot: snap})
	}
	patch := map[string]any{"tool_name": "apply_patch", "tool_use_id": "call_patch1", "tool_input": map[string]any{"command": "*** Begin Patch\n*** Update File: calc.go\n@@\n-a\n+b\n*** End Patch"}}
	test := map[string]any{"tool_name": "Bash", "tool_use_id": "call_test1", "tool_input": map[string]any{"command": "go test ./..."}, "tool_response": "ok"}
	add(0, "SessionStart", "c0", map[string]any{"source": "startup"})
	add(0.4, "UserPromptSubmit", "c0", map[string]any{"prompt": "Fix the Add function in calc.go and run the tests."})
	add(4.5, "PreToolUse", "", patch)
	add(4.8, "PostToolUse", "c1", patch)
	add(6.5, "PreToolUse", "", test)
	add(8.0, "PostToolUse", "c2", test)
	add(8.6, "Stop", "", map[string]any{"last_assistant_message": "Fixed Add in calc.go. The tests pass."})
	return out
}

func TestLiveCodexMergesRollout(t *testing.T) {
	tr := liveCodex(claudecode.Input{Events: codexEvents(codexRollout)}, codexRollout)
	s := tr.Session
	if s.Agent != "codex" || s.Fidelity != model.FidelityLive || s.AgentVersion != "0.47.0" || len(s.Models) != 1 || s.OutputTokens != 812 {
		t.Errorf("session = %+v", s)
	}
	kinds := map[model.StepKind]int{}
	for i, st := range tr.Steps {
		kinds[st.Kind]++
		if st.Seq != i+1 || (i > 0 && st.TsStart.Before(tr.Steps[i-1].TsStart)) {
			t.Errorf("step %d out of order: %+v", i, st)
		}
	}
	// One prompt from the hook, two assistant messages from the rollout and
	// not the Stop message again, four tool calls: two hooked, two only in the rollout.
	if kinds[model.StepUserMessage] != 1 || kinds[model.StepAssistantMessage] != 2 || kinds[model.StepToolCall] != 4 || len(tr.ToolCalls) != 4 {
		t.Errorf("kinds = %v, %d tool calls", kinds, len(tr.ToolCalls))
	}
	steps := map[string]model.Step{}
	for _, st := range tr.Steps {
		steps[st.ID] = st
	}
	for _, tc := range tr.ToolCalls {
		switch tc.ToolUseID {
		case "call_test1":
			if tc.ExitCode == nil || *tc.ExitCode != 0 || tc.Category != model.CatShell || !tc.OK {
				t.Errorf("test call = %+v", tc)
			}
		case "call_patch1":
			if tc.Category != model.CatEdit || steps[tc.StepID].Text != "apply_patch calc.go" || steps[tc.StepID].Snapshot != "c1" {
				t.Errorf("patch call = %+v, step %+v", tc, steps[tc.StepID])
			}
		}
	}
	if len(tr.Edits) != 0 {
		t.Errorf("edits come from the snapshots, not from the patch: %d", len(tr.Edits))
	}
	for _, m := range tr.Snapshots {
		if _, ok := steps[m.StepID]; !ok && m.StepID != "" {
			t.Errorf("snapshot %s points to a missing step", m.Commit)
		}
	}
}

func TestLiveCodexWithoutRolloutNeverGuessesPass(t *testing.T) {
	tr := liveCodex(claudecode.Input{Events: codexEvents("/nonexistent.jsonl")}, "/nonexistent.jsonl")
	if len(tr.Warnings) == 0 || len(tr.ToolCalls) != 2 {
		t.Fatalf("warnings %v, %d tool calls", tr.Warnings, len(tr.ToolCalls))
	}
	for _, tc := range tr.ToolCalls {
		if tc.ExitCode != nil {
			t.Errorf("%s has exit code %d without the rollout", tc.ToolUseID, *tc.ExitCode)
		}
	}
}

func TestLiveCodexCodeModeHasNoExecSteps(t *testing.T) {
	rollout := filepath.Join(t.TempDir(), "rollout.jsonl")
	lines := []string{
		`{"timestamp":"2026-10-20T09:00:00.000Z","type":"session_meta","payload":{"id":"019a2b3c-4d5e-7f60-8a9b-0c1d2e3f4a5b","cwd":"/work/calc","cli_version":"0.164.0"}}`,
		`{"timestamp":"2026-10-20T09:00:06.000Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec","call_id":"call_x","input":"const r = await tools.exec_command({cmd:\"go test ./...\"});"}}`,
		`{"timestamp":"2026-10-20T09:00:08.000Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call_x","output":"ok"}}`,
		`{"timestamp":"2026-10-20T09:00:08.500Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"The tests pass."}]}}`,
	}
	if err := os.WriteFile(rollout, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tr := liveCodex(claudecode.Input{Events: codexEvents(rollout)}, rollout)
	for _, tc := range tr.ToolCalls {
		if tc.ToolName == "exec" {
			t.Errorf("the exec wrapper became a tool call: %+v", tc)
		}
	}
	msgs := 0
	for _, st := range tr.Steps {
		if st.Kind == model.StepAssistantMessage {
			msgs++
		}
	}
	if len(tr.ToolCalls) != 2 || msgs != 1 {
		t.Errorf("%d tool calls, %d agent messages; want the 2 hooked calls and 1 message", len(tr.ToolCalls), msgs)
	}
}
