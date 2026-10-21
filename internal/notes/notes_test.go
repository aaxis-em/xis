package notes

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aaxis-em/xis/internal/model"
)

func TestSignVerifyTamper(t *testing.T) {
	key, created, err := LoadOrCreateKey(t.TempDir())
	if err != nil || !created {
		t.Fatal(err)
	}
	n := Note{Sessions: []NoteSession{{ID: "S", Agent: "claude-code", Models: []string{"m"}, Fidelity: "live"}},
		Hunks: []NoteHunk{{File: "a.go", NewStart: 1, NewLines: 2, ContentHash: "murmur3:00ff00ff", Steps: 1, Contributor: "ai"}},
		Flags: NoteFlags{High: 1, Suppressed: []Suppressed{}}, Review: NoteReview{Units: 2, Understood: 1}, XisVersion: "v0.1.0", CreatedAt: "2026-10-20T12:00:00Z"}
	raw, err := Sign(n, key)
	if err != nil {
		t.Fatal(err)
	}
	got, trusted, err := Verify(raw, PublicKey(key))
	if err != nil || !trusted || got.Hunks[0].ContentHash != "murmur3:00ff00ff" {
		t.Fatalf("verify: %v %v %+v", err, trusted, got)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	pretty, _ := json.MarshalIndent(m, "", "    ")
	if _, _, err := Verify(pretty, PublicKey(key)); err != nil {
		t.Errorf("reformatted note must still verify: %v", err)
	}
	bad := []byte(strings.Replace(string(raw), `"high":1`, `"high":0`, 1))
	if _, _, err := Verify(bad, PublicKey(key)); !errors.Is(err, ErrBadSignature) {
		t.Errorf("tampered note: %v", err)
	}
	other, _, _ := ed25519.GenerateKey(nil)
	if _, trusted, err := Verify(raw, other); err != nil || trusted {
		t.Errorf("foreign key: trusted=%v err=%v", trusted, err)
	}
}

func TestAgentTraceRanges(t *testing.T) {
	h := model.Hunk{ID: "h", FilePath: "a.go", NewStart: 10, Patch: "@@ -10,2 +10,4 @@\n ctx\n+one\n+two\n-old\n+three\n ctx"}
	rec := AgentTrace(TraceInput{
		Hunks:       []model.Hunk{h},
		LineSteps:   map[string]map[int]string{"a.go": {11: "s1", 12: "s1", 13: "s2"}},
		StepSession: map[string]string{"s1": "S", "s2": "S"}, ActorKind: map[string]string{"s1": "agent", "s2": "human"},
		Sessions: []model.Session{{ID: "S", Models: []string{"claude-sonnet-5-5"}}}, Now: time.Unix(0, 0),
	})
	if len(rec.Files) != 1 || len(rec.Files[0].Conversations) != 2 {
		t.Fatalf("%+v", rec)
	}
	ai := rec.Files[0].Conversations[0]
	if ai.Contributor.Type != "ai" || ai.Contributor.ModelID != "anthropic/claude-sonnet-5-5" || ai.Ranges[0].StartLine != 11 || ai.Ranges[0].EndLine != 12 {
		t.Errorf("ai conversation %+v", ai)
	}
	hu := rec.Files[0].Conversations[1]
	if hu.Contributor.Type != "human" || hu.Ranges[0].StartLine != 13 {
		t.Errorf("human conversation %+v", hu)
	}
	if len(rec.ID) != 36 || rec.Version != "0.1" || rec.VCS != nil {
		t.Errorf("record header %+v", rec)
	}
}
