package llm

import (
	"context"
	"errors"
	"testing"

	"github.com/aaxis-em/xis/internal/config"
)

func TestClientCacheBudgetRetry(t *testing.T) {
	m := NewMock()
	calls := 0
	m.Respond = func(r Request) (string, error) {
		calls++
		if calls == 1 {
			return "not json", nil
		}
		return "```json\n{\"ok\":true}\n```", nil
	}
	c := &Client{Provider: m, Cache: Cache{Dir: t.TempDir()}, Budget: &Budget{MaxUSD: 1, InPerMTok: 3, OutPerMT: 15}}
	req := Request{Task: "t", System: "s", Messages: []Message{{Role: "user", Content: "hello"}}, MaxTokens: 100}
	var out struct{ OK bool }
	if _, err := c.CompleteJSON(context.Background(), req, &out); err != nil || !out.OK {
		t.Fatalf("retry: %v %+v", err, out)
	}
	if calls != 2 {
		t.Errorf("calls = %d", calls)
	}
	retry := append(append([]Message{}, req.Messages...), Message{Role: "assistant", Content: "not json"})
	if len(m.All()[1].Messages) != 3 || m.All()[1].Messages[1] != retry[1] {
		t.Errorf("retry request = %+v", m.All()[1].Messages)
	}

	c2 := &Client{Provider: m, Cache: c.Cache, Budget: &Budget{MaxUSD: 1, InPerMTok: 3, OutPerMT: 15}}
	r, err := c2.Complete(context.Background(), Request{Task: "x", Messages: []Message{{Content: "a"}}, MaxTokens: 10})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := c2.Complete(context.Background(), Request{Task: "x", Messages: []Message{{Content: "a"}}, MaxTokens: 10})
	if err != nil || !r2.Cached || r2.Text != r.Text || c2.Budget.CacheHits != 1 {
		t.Errorf("cache: %+v %v", r2, err)
	}

	tight := &Client{Provider: m, Budget: &Budget{MaxUSD: 0.000001, InPerMTok: 3, OutPerMT: 15}}
	if _, err := tight.Complete(context.Background(), Request{Messages: []Message{{Content: "a long prompt"}}, MaxTokens: 1000}); !errors.Is(err, ErrBudget) {
		t.Errorf("budget: %v", err)
	}
}

func TestNewOff(t *testing.T) {
	t.Setenv("XIS_TEST_NO_KEY", "")
	if _, err := New(config.LLM{Provider: "auto", APIKeyEnv: "XIS_TEST_NO_KEY"}); !errors.Is(err, ErrOff) {
		t.Errorf("auto without key: %v", err)
	}
	if _, err := New(config.LLM{Provider: "openaicompat", Model: "m"}); err == nil {
		t.Error("openaicompat without base_url must fail")
	}
}
