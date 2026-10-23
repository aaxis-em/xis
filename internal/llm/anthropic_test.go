package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aaxis-em/xis/internal/config"
)

func TestAnthropicProvider(t *testing.T) {
	message := func(stop, extra string) string {
		return `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5-5",
			"content":[{"type":"text","text":"hi"}],"stop_reason":"` + stop + `"` + extra + `,
			"usage":{"input_tokens":10,"cache_creation_input_tokens":2,"cache_read_input_tokens":3,"output_tokens":5}}`
	}
	cases := []struct {
		name, status, body, wantErr string
	}{
		{"answer", "200", message("end_turn", ""), ""},
		{"refusal", "200", message("refusal", `,"stop_details":{"type":"refusal","category":"cyber","explanation":"no"}`), `refusal, category "cyber"`},
		{"length limit", "200", message("max_tokens", ""), "hit llm.max_tokens (100)"},
		{"api error", "400", `{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`, "anthropic API error 400"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var sent map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&sent)
				w.Header().Set("Content-Type", "application/json")
				if c.status == "400" {
					w.WriteHeader(http.StatusBadRequest)
				}
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			t.Setenv("XIS_TEST_ANTHROPIC_KEY", "k")
			p, err := newAnthropic(config.LLM{Model: "claude-sonnet-5-5", BaseURL: srv.URL, APIKeyEnv: "XIS_TEST_ANTHROPIC_KEY", MaxTokens: 100})
			if err != nil {
				t.Fatal(err)
			}
			r, err := p.Complete(context.Background(), Request{System: "s", Messages: []Message{{Role: "user", Content: "q"}}, Schema: map[string]any{"type": "object"}})
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v, want %q", err, c.wantErr)
				}
				return
			}
			if err != nil || r.Text != "hi" || r.InputTokens != 15 || r.OutputTokens != 5 {
				t.Fatalf("Complete = %+v, %v", r, err)
			}
			if _, ok := sent["temperature"]; ok {
				t.Error("request sends temperature, which current Claude models reject")
			}
			if f, _ := sent["output_config"].(map[string]any)["format"].(map[string]any); f["schema"] == nil {
				t.Errorf("request has no JSON schema: %v", sent["output_config"])
			}
		})
	}
}
