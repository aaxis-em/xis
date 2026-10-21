package llm

import (
	"context"
	"sync"
)

// Mock is a deterministic provider for tests. Respond decides the reply; it
// records every request so tests can check what would leave the machine.
type Mock struct {
	mu       sync.Mutex
	Requests []Request
	Respond  func(Request) (string, error)
}

func NewMock() *Mock {
	return &Mock{Respond: func(Request) (string, error) { return "{}", nil }}
}

func (m *Mock) Name() string  { return "mock" }
func (m *Mock) Model() string { return "mock-1" }

func (m *Mock) Complete(_ context.Context, req Request) (Response, error) {
	m.mu.Lock()
	m.Requests = append(m.Requests, req)
	respond := m.Respond
	m.mu.Unlock()
	text, err := respond(req)
	if err != nil {
		return Response{}, err
	}
	return Response{Text: text, Model: "mock-1", InputTokens: EstimateTokens(req), OutputTokens: int64(len(text)/4 + 1)}, nil
}

func (m *Mock) All() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Request{}, m.Requests...)
}
