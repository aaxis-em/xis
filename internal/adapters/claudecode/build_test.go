package claudecode_test

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/aaxis-em/xis/internal/adapters/claudecode"
	"github.com/aaxis-em/xis/internal/model"
)

var update = flag.Bool("update", false, "rewrite golden files")

const fixtures = "../../../testdata/fixtures/claudecode"

func readSpool(t *testing.T, path string) []model.HookEvent {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []model.HookEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var l struct {
			Ts       time.Time       `json:"ts"`
			Event    string          `json:"event"`
			Payload  json.RawMessage `json:"payload"`
			Snapshot *string         `json:"snapshot"`
		}
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(sc.Bytes())
		ev := model.HookEvent{Key: hex.EncodeToString(sum[:8]), Seq: len(out), Ts: l.Ts, Event: l.Event, Payload: string(l.Payload)}
		if l.Snapshot != nil {
			ev.Snapshot = *l.Snapshot
		}
		out = append(out, ev)
	}
	return out
}

func readHookPayloads(t *testing.T, path, session string) []model.HookEvent {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []model.HookEvent
	ts := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for i, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		p, err := claudecode.ParseHookPayload([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		if p.SessionID != session {
			continue
		}
		out = append(out, model.HookEvent{Key: strings.Repeat("0", 15) + string(rune('a'+i%26)), Seq: i, Ts: ts.Add(time.Duration(i) * time.Second), Event: p.HookEventName, Payload: line})
	}
	return out
}

func golden(t *testing.T, name string, got any) {
	t.Helper()
	b, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", name+".golden.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if diff := cmp.Diff(strings.TrimSpace(string(want)), strings.TrimSpace(string(b))); diff != "" {
		t.Errorf("golden %s mismatch (-want +got):\n%s", name, diff)
	}
}

func TestLiveSessionGolden(t *testing.T) {
	entries, warns, err := claudecode.ReadTranscript(filepath.Join(fixtures, "live-session", "transcript.jsonl"))
	if err != nil || len(warns) > 0 {
		t.Fatal(err, warns)
	}
	tr := claudecode.Build(claudecode.Input{
		Events:        readSpool(t, filepath.Join(fixtures, "live-session", "spool.jsonl")),
		Transcript:    entries,
		HasTranscript: true,
	})
	golden(t, "live-session", tr)

	kinds := map[model.StepKind]int{}
	for _, s := range tr.Steps {
		kinds[s.Kind]++
	}
	if kinds[model.StepUserMessage] != 1 || kinds[model.StepToolCall] != 6 || kinds[model.StepHumanEdit] != 2 {
		t.Errorf("kinds = %v", kinds)
	}
	if len(tr.Snapshots) != 6 || !tr.Snapshots[0].Base {
		t.Errorf("snapshots = %+v", tr.Snapshots)
	}
	for _, tc := range tr.ToolCalls {
		if tc.ToolName == "Bash" && (tc.ExitCode == nil || *tc.ExitCode != 0) {
			t.Errorf("bash exit code = %v", tc.ExitCode)
		}
	}
	if tr.Session.Status != "ended" || tr.Session.AgentVersion != "2.1.304" || len(tr.Session.Models) == 0 {
		t.Errorf("session = %+v", tr.Session)
	}
}

func TestFailureSubagentGolden(t *testing.T) {
	dir := filepath.Join(fixtures, "failure-subagent")
	entries, _, err := claudecode.ReadTranscript(filepath.Join(dir, "transcript.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	sub, _, err := claudecode.ReadTranscript(filepath.Join(dir, "subagents", "agent-ad54eebf8c0be04c7.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	events := readHookPayloads(t, filepath.Join(fixtures, "hook-payloads.jsonl"), "fac244f5-7aa1-4559-acfd-f596f195a466")
	tr := claudecode.Build(claudecode.Input{Events: events, Transcript: entries, Subagents: map[string][]claudecode.Entry{"ad54eebf8c0be04c7": sub}, HasTranscript: true})
	golden(t, "failure-subagent", tr)

	var failed, subagentSteps int
	for _, tc := range tr.ToolCalls {
		if tc.ToolName == "Bash" && !tc.OK {
			failed++
			if tc.ExitCode == nil || *tc.ExitCode != 1 {
				t.Errorf("failed bash exit = %v", tc.ExitCode)
			}
		}
	}
	for _, s := range tr.Steps {
		if s.ParentStepID != "" {
			subagentSteps++
		}
	}
	if failed != 1 || subagentSteps == 0 || len(tr.Actors) != 3 {
		t.Errorf("failed=%d subagentSteps=%d actors=%d", failed, subagentSteps, len(tr.Actors))
	}
}
