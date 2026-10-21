package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeOAI struct {
	mu       sync.Mutex
	bodies   []map[string]any
	reject   func(body map[string]any) (int, string)
	busy     int
	response string
}

func (f *fakeOAI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	f.bodies = append(f.bodies, body)
	busy := f.busy > 0
	if busy {
		f.busy--
	}
	f.mu.Unlock()
	if busy {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limit reached"}}`))
		return
	}
	if f.reject != nil {
		if code, msg := f.reject(body); code != 0 {
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": msg}})
			return
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"model":   "served-model",
		"choices": []any{map[string]any{"message": map[string]any{"content": f.response}, "finish_reason": "stop"}},
		"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
	})
}

func newFake(t *testing.T, f *fakeOAI) *openAICompat {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return &openAICompat{baseURL: srv.URL, model: "m", maxTokens: 100, http: srv.Client(), retryBase: time.Millisecond, retries: 3}
}

func formatType(b map[string]any) string {
	rf, ok := b["response_format"].(map[string]any)
	if !ok {
		return "none"
	}
	if rf["type"] == "json_schema" && rf["json_schema"].(map[string]any)["strict"] == true {
		return "strict"
	}
	return rf["type"].(string)
}

var schemaReq = Request{Task: "t", System: "sys", Messages: []Message{{Role: "user", Content: "hi"}}, Schema: map[string]any{"type": "object"}}

func TestOpenAICompatFallsBack(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reject func(map[string]any) (int, string)
		want   []string
	}{
		{"accepts everything", nil, []string{"strict"}},
		{"no strict", func(b map[string]any) (int, string) {
			if formatType(b) == "strict" {
				return 400, "strict mode is not supported"
			}
			return 0, ""
		}, []string{"strict", "json_schema"}},
		{"json_object only", func(b map[string]any) (int, string) {
			if strings.HasPrefix(formatType(b), "s") || formatType(b) == "json_schema" {
				return 400, "`response_format.type` json_schema is not supported with this model"
			}
			return 0, ""
		}, []string{"strict", "json_schema", "json_object"}},
		{"no response_format at all", func(b map[string]any) (int, string) {
			if _, ok := b["response_format"]; ok {
				return 422, "unknown field response_format"
			}
			return 0, ""
		}, []string{"strict", "json_schema", "json_object", "none"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeOAI{reject: tc.reject, response: `{"ok":true}`}
			p := newFake(t, f)
			r, err := p.Complete(context.Background(), schemaReq)
			if err != nil || r.Text != `{"ok":true}` || r.Model != "served-model" {
				t.Fatalf("Complete = %+v, %v", r, err)
			}
			var got []string
			for _, b := range f.bodies {
				got = append(got, formatType(b))
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("formats = %v, want %v", got, tc.want)
			}
			last := f.bodies[len(f.bodies)-1]
			sys := last["messages"].([]any)[0].(map[string]any)["content"].(string)
			if inPrompt := strings.Contains(sys, "JSON schema of the reply"); inPrompt != (formatType(last) == "json_object" || formatType(last) == "none") {
				t.Errorf("schema in system prompt = %v for format %s", inPrompt, formatType(last))
			}
			if _, err := p.Complete(context.Background(), schemaReq); err != nil || len(f.bodies) != len(tc.want)+1 {
				t.Errorf("second call did not reuse the learned format: %d requests, %v", len(f.bodies), err)
			}
		})
	}
}

func TestOpenAICompatReasoningModelParams(t *testing.T) {
	f := &fakeOAI{response: "ok", reject: func(b map[string]any) (int, string) {
		if _, ok := b["temperature"]; ok {
			return 400, "Unsupported value: 'temperature' does not support 0 with this model. Only the default (1) value is supported."
		}
		if _, ok := b["max_tokens"]; ok {
			return 400, "Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead."
		}
		return 0, ""
	}}
	p := newFake(t, f)
	if _, err := p.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatal(err)
	}
	last := f.bodies[len(f.bodies)-1]
	if _, ok := last["temperature"]; ok || last["max_completion_tokens"] != float64(100) || len(f.bodies) != 3 {
		t.Errorf("requests = %d, last = %v", len(f.bodies), last)
	}
}

func TestOpenAICompatStopsOnOtherErrors(t *testing.T) {
	f := &fakeOAI{reject: func(map[string]any) (int, string) { return 400, "context length exceeded" }}
	p := newFake(t, f)
	_, err := p.Complete(context.Background(), schemaReq)
	if err == nil || !strings.Contains(err.Error(), "context length exceeded") || len(f.bodies) != 1 {
		t.Errorf("err = %v after %d requests", err, len(f.bodies))
	}

	f2 := &fakeOAI{reject: func(map[string]any) (int, string) { return 401, "invalid api key" }}
	if _, err := newFake(t, f2).Complete(context.Background(), schemaReq); err == nil || len(f2.bodies) != 1 {
		t.Errorf("401: err = %v after %d requests", err, len(f2.bodies))
	}
}

func TestOpenAICompatRetriesRateLimit(t *testing.T) {
	f := &fakeOAI{busy: 2, response: "ok"}
	if r, err := newFake(t, f).Complete(context.Background(), Request{}); err != nil || r.Text != "ok" || len(f.bodies) != 3 {
		t.Errorf("Complete = %+v, %v after %d requests", r, err, len(f.bodies))
	}
	f2 := &fakeOAI{busy: 10, response: "ok"}
	if _, err := newFake(t, f2).Complete(context.Background(), Request{}); err == nil || !strings.Contains(err.Error(), "429") || len(f2.bodies) != 4 {
		t.Errorf("err = %v after %d requests", err, len(f2.bodies))
	}
}

func TestExtractJSON(t *testing.T) {
	for in, want := range map[string]string{
		`{"a":1}`:                 `{"a":1}`,
		"```json\n{\"a\":1}\n```": `{"a":1}`,
		"<think>use {braces} here</think>\n{\"a\":1}":       `{"a":1}`,
		"reasoning without an open tag</think>{\"a\":1}":    `{"a":1}`,
		"Here is the JSON:\n```json\n{\"a\":1}\n```\nDone.": `{"a":1}`,
		"Sure! {\"a\":{\"b\":2}} Hope this helps.":          `{"a":{"b":2}}`,
		`[1,2]`: `[1,2]`,
	} {
		if got := extractJSON(in); got != want {
			t.Errorf("extractJSON(%q) = %q, want %q", in, got, want)
		}
	}
}
