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
	"strings"
	"time"

	"github.com/aaxis-em/xis/internal/config"
)

type openAICompat struct {
	baseURL   string
	apiKey    string
	model     string
	maxTokens int
	http      *http.Client
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
	}, nil
}

func (p *openAICompat) Name() string  { return "openaicompat" }
func (p *openAICompat) Model() string { return p.model }

type oaiRequest struct {
	Model          string         `json:"model"`
	Messages       []Message      `json:"messages"`
	Temperature    float64        `json:"temperature"`
	MaxTokens      int            `json:"max_tokens,omitempty"`
	ResponseFormat map[string]any `json:"response_format,omitempty"`
}

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

func (p *openAICompat) Complete(ctx context.Context, req Request) (Response, error) {
	body := oaiRequest{Model: p.model, Temperature: req.Temperature, MaxTokens: req.MaxTokens}
	if body.MaxTokens <= 0 {
		body.MaxTokens = p.maxTokens
	}
	body.Messages = append([]Message{{Role: "system", Content: req.System}}, req.Messages...)
	if req.Schema != nil {
		body.ResponseFormat = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "xis_" + req.Task, "schema": req.Schema, "strict": true}}
	}
	b, _ := json.Marshal(body)
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return Response{}, err
	}
	hreq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		hreq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.http.Do(hreq)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return Response{}, err
	}
	var out oaiResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return Response{}, fmt.Errorf("openaicompat: HTTP %d: %s", resp.StatusCode, firstLine(string(raw)))
	}
	if resp.StatusCode >= 300 || out.Error != nil {
		msg := string(raw)
		if out.Error != nil {
			msg = out.Error.Message
		}
		return Response{}, fmt.Errorf("openaicompat: HTTP %d: %s", resp.StatusCode, firstLine(msg))
	}
	if len(out.Choices) == 0 {
		return Response{}, errors.New("openaicompat: no choices in response")
	}
	if out.Choices[0].FinishReason == "length" {
		return Response{}, fmt.Errorf("the response hit llm.max_tokens (%d); raise it", body.MaxTokens)
	}
	model := out.Model
	if model == "" {
		model = p.model
	}
	return Response{Text: out.Choices[0].Message.Content, Model: model, InputTokens: out.Usage.PromptTokens, OutputTokens: out.Usage.CompletionTokens}, nil
}
