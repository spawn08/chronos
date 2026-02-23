package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Mistral implements Provider for the Mistral AI API.
// Mistral uses an OpenAI-compatible chat completions format.
type Mistral struct {
	config ProviderConfig
	http   *httpClient
}

// NewMistral creates a new Mistral provider with the given API key.
func NewMistral(apiKey string) *Mistral {
	return NewMistralWithConfig(ProviderConfig{
		APIKey:  apiKey,
		BaseURL: "https://api.mistral.ai/v1",
		Model:   "mistral-large-latest",
	})
}

// NewMistralWithConfig creates a Mistral provider with full configuration.
func NewMistralWithConfig(cfg ProviderConfig) *Mistral {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.mistral.ai/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "mistral-large-latest"
	}
	headers := map[string]string{
		"Authorization": "Bearer " + cfg.APIKey,
	}
	return &Mistral{
		config: cfg,
		http:   newHTTPClient(cfg.BaseURL, cfg.TimeoutSec, headers),
	}
}

func (m *Mistral) Name() string  { return "mistral" }
func (m *Mistral) Model() string { return m.config.Model }

func (m *Mistral) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body := buildOpenAIRequestBody(req, m.config.Model, false)

	resp, err := m.http.post(ctx, "/chat/completions", body)
	if err != nil {
		return nil, fmt.Errorf("mistral chat: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mistral chat: %s", readErrorBody(resp))
	}

	var oaiResp openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("mistral chat decode: %w", err)
	}
	return convertOpenAIResponse(&oaiResp), nil
}

func (m *Mistral) StreamChat(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error) {
	body := buildOpenAIRequestBody(req, m.config.Model, true)

	resp, err := m.http.post(ctx, "/chat/completions", body)
	if err != nil {
		return nil, fmt.Errorf("mistral stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := readErrorBody(resp)
		resp.Body.Close()
		return nil, fmt.Errorf("mistral stream: %s", errMsg)
	}

	ch := make(chan *ChatResponse, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		readOpenAISSEStream(resp, ch)
	}()
	return ch, nil
}
