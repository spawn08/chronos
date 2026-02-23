package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Ollama implements Provider for locally running Ollama models.
// Ollama exposes an OpenAI-compatible API at /v1/chat/completions.
type Ollama struct {
	config ProviderConfig
	http   *httpClient
}

// NewOllama creates a new Ollama provider pointing at a local instance.
// host is the Ollama server address (e.g., "http://localhost:11434").
// modelName is the model tag (e.g., "llama3.2", "mistral", "codellama").
func NewOllama(host, modelName string) *Ollama {
	return NewOllamaWithConfig(ProviderConfig{
		BaseURL: host,
		Model:   modelName,
	})
}

// NewOllamaWithConfig creates an Ollama provider with full configuration.
func NewOllamaWithConfig(cfg ProviderConfig) *Ollama {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:11434"
	}
	if cfg.Model == "" {
		cfg.Model = "llama3.2"
	}
	return &Ollama{
		config: cfg,
		http:   newHTTPClient(cfg.BaseURL, cfg.TimeoutSec, nil),
	}
}

func (o *Ollama) Name() string  { return "ollama" }
func (o *Ollama) Model() string { return o.config.Model }

func (o *Ollama) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body := buildOpenAIRequestBody(req, o.config.Model, false)

	resp, err := o.http.post(ctx, "/v1/chat/completions", body)
	if err != nil {
		return nil, fmt.Errorf("ollama chat: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama chat: %s", readErrorBody(resp))
	}

	var oaiResp openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("ollama chat decode: %w", err)
	}
	return convertOpenAIResponse(&oaiResp), nil
}

func (o *Ollama) StreamChat(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error) {
	body := buildOpenAIRequestBody(req, o.config.Model, true)

	resp, err := o.http.post(ctx, "/v1/chat/completions", body)
	if err != nil {
		return nil, fmt.Errorf("ollama stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := readErrorBody(resp)
		resp.Body.Close()
		return nil, fmt.Errorf("ollama stream: %s", errMsg)
	}

	ch := make(chan *ChatResponse, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		readOpenAISSEStream(resp, ch)
	}()
	return ch, nil
}
