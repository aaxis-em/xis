package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aaxis-em/xis/internal/config"
	"github.com/aaxis-em/xis/internal/textx"
)

type jsonMode int

const (
	modeStrictSchema jsonMode = iota
	modeSchema
	modeObject
	modeText
)

// oaiQuirks are the request features a server rejected. The provider learns
// them on the first call and keeps them for the next calls.
type oaiQuirks struct {
	mode             jsonMode
	noTemperature    bool
	completionTokens bool
}

type openAICompat struct {
	baseURL   string
	apiKey    string
	model     string
	maxTokens int
	http      *http.Client
	retryBase time.Duration
	retries   int

	mu     sync.Mutex
	quirks oaiQuirks
}

func newOpenAICompat(cfg config.LLM) (Provider, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("llm.base_url is empty (for Ollama use http://localhost:11434/v1)")
	}
	if cfg.Model == "" {
		return nil, errors.New("llm.model is empty")
	}
	return &openAICompat{
		baseURL: strings.TrimSuffix(cfg.BaseURL, "/"), apiKey: os.Getenv(cfg.APIKeyEnv), model: cfg.Model,
		maxTokens: cfg.MaxTokens, http: &http.Client{Timeout: 10 * time.Minute},
		retryBase: 2 * time.Second, retries: 3,
	}, nil
}

func (p *openAICompat) Name() string  { return "openaicompat" }
func (p *openAICompat) Model() string { return p.model }

type oaiResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type oaiHTTPError struct {
	status     int
	msg        string
	retryAfter time.Duration
}

func (e *oaiHTTPError) Error() string {
	return fmt.Sprintf("openaicompat: HTTP %d: %s", e.status, textx.FirstLine(e.msg))
}

func (p *openAICompat) Complete(ctx context.Context, req Request) (Response, error) {
	p.mu.Lock()
	q := p.quirks
	p.mu.Unlock()
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = p.maxTokens
	}
	busy := 0
	for {
		out, err := p.send(ctx, p.body(req, q, maxTokens))
		var he *oaiHTTPError
		switch {
		case err == nil:
			p.mu.Lock()
			p.quirks = q
			p.mu.Unlock()
			return p.response(out, maxTokens)
		case !errors.As(err, &he):
			return Response{}, err
		case (he.status == http.StatusBadRequest || he.status == http.StatusUnprocessableEntity) && q.adapt(he.msg, req.Schema != nil):
			continue
		case (he.status == http.StatusTooManyRequests || he.status >= 500) && busy < p.retries:
			busy++
			if err := sleep(ctx, p.backoff(busy, he.retryAfter)); err != nil {
				return Response{}, err
			}
			continue
		}
		return Response{}, err
	}
}

// adapt drops the feature that the server's error message names. It returns
// false when the message names no feature that is left to drop.
func (q *oaiQuirks) adapt(msg string, schema bool) bool {
	m := strings.ToLower(msg)
	switch {
	case !q.noTemperature && strings.Contains(m, "temperature"):
		q.noTemperature = true
	case !q.completionTokens && strings.Contains(m, "max_completion_tokens"):
		q.completionTokens = true
	case schema && q.mode < modeText && containsAny(m, "response_format", "json_schema", "json_object", "schema", "strict", "json mode"):
		q.mode++
	default:
		return false
	}
	return true
}

func (p *openAICompat) body(req Request, q oaiQuirks, maxTokens int) map[string]any {
	system := req.System
	if req.Schema != nil && q.mode >= modeObject {
		s, _ := json.Marshal(req.Schema)
		system += "\n\nThe JSON schema of the reply:\n" + string(s)
	}
	b := map[string]any{
		"model":    p.model,
		"messages": append([]Message{{Role: "system", Content: system}}, req.Messages...),
	}
	if !q.noTemperature {
		b["temperature"] = req.Temperature
	}
	if maxTokens > 0 {
		if q.completionTokens {
			b["max_completion_tokens"] = maxTokens
		} else {
			b["max_tokens"] = maxTokens
		}
	}
	if req.Schema != nil {
		switch q.mode {
		case modeStrictSchema, modeSchema:
			b["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{
				"name": "xis_" + req.Task, "schema": req.Schema, "strict": q.mode == modeStrictSchema,
			}}
		case modeObject:
			b["response_format"] = map[string]any{"type": "json_object"}
		}
	}
	return b
}

func (p *openAICompat) send(ctx context.Context, body map[string]any) (*oaiResponse, error) {
	b, _ := json.Marshal(body)
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		hreq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.http.Do(hreq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	var out oaiResponse
	jsonErr := json.Unmarshal(raw, &out)
	if resp.StatusCode >= 300 || jsonErr != nil || out.Error != nil {
		msg := string(raw)
		if jsonErr == nil && out.Error != nil {
			msg = out.Error.Message
		}
		return nil, &oaiHTTPError{status: resp.StatusCode, msg: msg, retryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	}
	return &out, nil
}

func (p *openAICompat) response(out *oaiResponse, maxTokens int) (Response, error) {
	if len(out.Choices) == 0 {
		return Response{}, errors.New("openaicompat: no choices in response")
	}
	if out.Choices[0].FinishReason == "length" {
		return Response{}, fmt.Errorf("the response hit llm.max_tokens (%d); raise it", maxTokens)
	}
	model := out.Model
	if model == "" {
		model = p.model
	}
	return Response{Text: out.Choices[0].Message.Content, Model: model, InputTokens: out.Usage.PromptTokens, OutputTokens: out.Usage.CompletionTokens}, nil
}

func (p *openAICompat) backoff(attempt int, hint time.Duration) time.Duration {
	if hint > 0 {
		return min(hint, time.Minute)
	}
	return p.retryBase << (attempt - 1)
}

func retryAfter(h string) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return 0
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
