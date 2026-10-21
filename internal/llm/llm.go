package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/aaxis-em/xis/internal/config"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	Task          string         `json:"task"`
	PromptVersion string         `json:"prompt_version"`
	System        string         `json:"system"`
	Messages      []Message      `json:"messages"`
	Schema        map[string]any `json:"schema,omitempty"`
	MaxTokens     int            `json:"max_tokens"`
	Temperature   float64        `json:"temperature"`
}

type Response struct {
	Text         string
	Model        string
	InputTokens  int64
	OutputTokens int64
	Cached       bool
}

type Provider interface {
	Name() string
	Model() string
	Complete(ctx context.Context, req Request) (Response, error)
}

var (
	ErrOff    = errors.New("LLM analysis is off (llm.provider = none)")
	ErrBudget = errors.New("LLM budget for this change is used up")
)

func New(cfg config.LLM) (Provider, error) {
	switch cfg.EffectiveProvider() {
	case "none":
		return nil, ErrOff
	case "anthropic":
		return newAnthropic(cfg)
	case "openaicompat":
		return newOpenAICompat(cfg)
	case "mock":
		return NewMock(), nil
	}
	return nil, fmt.Errorf("unknown llm.provider %q", cfg.Provider)
}

type Budget struct {
	mu        sync.Mutex
	MaxUSD    float64
	InPerMTok float64
	OutPerMT  float64
	SpentUSD  float64
	Calls     int
	CacheHits int
}

func NewBudget(cfg config.LLM) *Budget {
	return &Budget{MaxUSD: cfg.MaxUSDPerChange, InPerMTok: cfg.PriceInputPerMTok, OutPerMT: cfg.PriceOutputPerMTok}
}

func (b *Budget) cost(in, out int64) float64 {
	return float64(in)/1e6*b.InPerMTok + float64(out)/1e6*b.OutPerMT
}

// EstimateTokens uses about 4 characters per token, which is close enough
// for a budget guard.
func EstimateTokens(req Request) int64 {
	n := len(req.System)
	for _, m := range req.Messages {
		n += len(m.Content)
	}
	if req.Schema != nil {
		b, _ := json.Marshal(req.Schema)
		n += len(b)
	}
	return int64(n/4 + 1)
}

func (b *Budget) Reserve(req Request) error {
	if b == nil || b.MaxUSD <= 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.SpentUSD+b.cost(EstimateTokens(req), int64(req.MaxTokens)) > b.MaxUSD {
		return fmt.Errorf("%w: spent $%.4f of $%.2f (llm.max_usd_per_change)", ErrBudget, b.SpentUSD, b.MaxUSD)
	}
	return nil
}

func (b *Budget) Add(r Response) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if r.Cached {
		b.CacheHits++
		return
	}
	b.Calls++
	b.SpentUSD += b.cost(r.InputTokens, r.OutputTokens)
}

type Cache struct{ Dir string }

func (c Cache) key(p Provider, req Request) string {
	b, _ := json.Marshal(struct {
		Provider, Model string
		Req             Request
	}{p.Name(), p.Model(), req})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (c Cache) path(key string) string { return filepath.Join(c.Dir, key[:2], key+".json") }

func (c Cache) Get(p Provider, req Request) (Response, bool) {
	if c.Dir == "" {
		return Response{}, false
	}
	b, err := os.ReadFile(c.path(c.key(p, req)))
	if err != nil {
		return Response{}, false
	}
	var r Response
	if json.Unmarshal(b, &r) != nil {
		return Response{}, false
	}
	r.Cached = true
	return r, true
}

func (c Cache) Put(p Provider, req Request, r Response) {
	if c.Dir == "" {
		return
	}
	b, _ := json.Marshal(r)
	_ = config.WriteFile(c.path(c.key(p, req)), b)
}

// Client adds the cache, the budget guard and the JSON validation to a provider.
type Client struct {
	Provider Provider
	Cache    Cache
	Budget   *Budget
}

func (c *Client) Complete(ctx context.Context, req Request) (Response, error) {
	if r, ok := c.Cache.Get(c.Provider, req); ok {
		c.Budget.Add(r)
		return r, nil
	}
	if err := c.Budget.Reserve(req); err != nil {
		return Response{}, err
	}
	r, err := c.Provider.Complete(ctx, req)
	if err != nil {
		return Response{}, err
	}
	c.Budget.Add(r)
	c.Cache.Put(c.Provider, req, r)
	return r, nil
}

// CompleteJSON decodes the response into out. It retries once when the
// response is not valid JSON for out.
func (c *Client) CompleteJSON(ctx context.Context, req Request, out any) (Response, error) {
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		r, err := c.Complete(ctx, req)
		if err != nil {
			return r, err
		}
		if err = decodeStrict(r.Text, out); err == nil {
			return r, nil
		}
		last = err
		if r.Cached {
			_ = os.Remove(c.Cache.path(c.Cache.key(c.Provider, req)))
		}
		req.Messages = append(append([]Message{}, req.Messages...),
			Message{Role: "assistant", Content: r.Text},
			Message{Role: "user", Content: "The previous response was not valid JSON for the schema (" + err.Error() + "). Reply again with only the JSON object."})
	}
	return Response{}, fmt.Errorf("invalid JSON from the LLM: %w", last)
}

func decodeStrict(text string, out any) error {
	text = stripFence(text)
	if text == "" {
		return errors.New("empty response")
	}
	return json.Unmarshal([]byte(text), out)
}

func stripFence(s string) string {
	b := []byte(s)
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\n' || b[0] == '\r' || b[0] == '\t') {
		b = b[1:]
	}
	s = string(b)
	if len(s) > 3 && s[:3] == "```" {
		if i := indexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
		if j := lastIndex(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	return s
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func lastIndex(s, sub string) int {
	for i := len(s) - len(sub); i >= 0; i-- {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
