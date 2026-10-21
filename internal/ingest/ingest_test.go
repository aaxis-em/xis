package ingest

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aaxis-em/xis/internal/config"
	"github.com/aaxis-em/xis/internal/redact"
	"github.com/aaxis-em/xis/internal/store"
)

func TestStoredPayloadKeepsIDs(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	in, err := New(config.PathsAt(t.TempDir(), t.TempDir()), config.Default(), st)
	if err != nil {
		t.Fatal(err)
	}
	id := "toolu_015dd34kGCsGJ4CBb9vHM2y7"
	raw := `{"session_id":"9287fccd-7bef-438f-9387-8921f229846e","tool_use_id":"` + id + `","transcript_path":"/home/u/.claude/projects/x/9287fccd.jsonl",` +
		`"tool_input":{"command":"export K=` + redact.FakeSecrets["github"] + `"},"tool_response":{"stdout":"` + strings.Repeat("x", 40000) + `"}}`
	out := in.storedPayload(raw)
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatal(err)
	}
	if m["tool_use_id"] != id || m["session_id"] != "9287fccd-7bef-438f-9387-8921f229846e" {
		t.Errorf("IDs changed: %v %v", m["tool_use_id"], m["session_id"])
	}
	if strings.Contains(out, redact.FakeSecrets["github"]) {
		t.Error("secret kept in tool_input")
	}
	if len(out) > 34000 {
		t.Errorf("tool_response not truncated: %d bytes", len(out))
	}
}
