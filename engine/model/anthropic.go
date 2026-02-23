package model

import (
	"context"
	"fmt"
)

// Anthropic implements Provider for Claude models.
type Anthropic struct {
	APIKey  string
	BaseURL string
}

func NewAnthropic(apiKey string) *Anthropic {
	return &Anthropic{
		APIKey:  apiKey,
		BaseURL: "https://api.anthropic.com/v1",
	}
}

func (a *Anthropic) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	// TODO: Implement Anthropic Messages API call
	return nil, fmt.Errorf("anthropic: not yet implemented")
}

func (a *Anthropic) StreamChat(_ context.Context, _ *ChatRequest) (<-chan *ChatResponse, error) {
	return nil, fmt.Errorf("anthropic: streaming not yet implemented")
}
