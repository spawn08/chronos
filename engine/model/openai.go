package model

import (
	"context"
	"fmt"
)

// OpenAI implements Provider for OpenAI-compatible endpoints.
type OpenAI struct {
	APIKey  string
	BaseURL string
}

func NewOpenAI(apiKey string) *OpenAI {
	return &OpenAI{
		APIKey:  apiKey,
		BaseURL: "https://api.openai.com/v1",
	}
}

func (o *OpenAI) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	return nil, fmt.Errorf("openai: not yet implemented")
}

func (o *OpenAI) StreamChat(_ context.Context, _ *ChatRequest) (<-chan *ChatResponse, error) {
	return nil, fmt.Errorf("openai: streaming not yet implemented")
}
