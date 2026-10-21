package llm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/aaxis-em/xis/internal/config"
)

type anthropicProvider struct {
	client    anthropic.Client
	model     string
	maxTokens int
}

func newAnthropic(cfg config.LLM) (Provider, error) {
	opts := []option.RequestOption{}
	if key := os.Getenv(cfg.APIKeyEnv); key != "" {
		opts = append(opts, option.WithAPIKey(key))
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if cfg.Model == "" {
		return nil, errors.New("llm.model is empty")
	}
	return &anthropicProvider{client: anthropic.NewClient(opts...), model: cfg.Model, maxTokens: cfg.MaxTokens}, nil
}

func (p *anthropicProvider) Name() string  { return "anthropic" }
func (p *anthropicProvider) Model() string { return p.model }

// Complete sends no temperature: current Claude models reject non-default
// sampling values, and the response cache gives repeatable output instead.
func (p *anthropicProvider) Complete(ctx context.Context, req Request) (Response, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = p.maxTokens
	}
	params := anthropic.MessageNewParams{
		Model:     p.model,
		MaxTokens: int64(maxTokens),
		System:    []anthropic.TextBlockParam{{Text: req.System}},
	}
	for _, m := range req.Messages {
		if m.Role == "assistant" {
			params.Messages = append(params.Messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content)))
		} else {
			params.Messages = append(params.Messages, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		}
	}
	if req.Schema != nil {
		params.OutputConfig = anthropic.OutputConfigParam{Format: anthropic.JSONOutputFormatParam{Schema: req.Schema}}
	}
	resp, err := p.client.Messages.New(ctx, params)
	if err != nil {
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) {
			return Response{}, fmt.Errorf("anthropic API error %d: %s", apiErr.StatusCode, firstLine(apiErr.Error()))
		}
		return Response{}, err
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return Response{}, fmt.Errorf("the model declined the request (refusal, category %q)", resp.StopDetails.Category)
	}
	var text strings.Builder
	for _, b := range resp.Content {
		if t, ok := b.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(t.Text)
		}
	}
	if resp.StopReason == anthropic.StopReasonMaxTokens {
		return Response{}, fmt.Errorf("the response hit llm.max_tokens (%d); raise it", maxTokens)
	}
	in := resp.Usage.InputTokens + resp.Usage.CacheCreationInputTokens + resp.Usage.CacheReadInputTokens
	return Response{Text: text.String(), Model: resp.Model, InputTokens: in, OutputTokens: resp.Usage.OutputTokens}, nil
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}
