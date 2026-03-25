package evals

import (
	"context"
	"errors"

	"github.com/spawn08/chronos/engine/model"
)

// mockEvalProvider is a model.Provider for testing evals.
type mockEvalProvider struct {
	response string
	err      error
}

func (m *mockEvalProvider) Chat(_ context.Context, _ *model.ChatRequest) (*model.ChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &model.ChatResponse{Content: m.response}, nil
}

func (m *mockEvalProvider) StreamChat(_ context.Context, _ *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockEvalProvider) Name() string  { return "mock" }
func (m *mockEvalProvider) Model() string { return "mock-model" }
