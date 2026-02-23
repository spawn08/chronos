package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// OpenAICompatible implements Provider for any OpenAI-compatible API endpoint.
// Use this for self-hosted models (vLLM, TGI, LiteLLM), cloud providers
// that expose an OpenAI-compatible interface (Together, Anyscale, Fireworks,
// Groq, DeepSeek, OpenRouter, Perplexity), or any custom LLM server.
type OpenAICompatible struct {
	config       ProviderConfig
	providerName string
	http         *httpClient
}

// NewOpenAICompatible creates a provider for any OpenAI-compatible endpoint.
// name is a human-readable identifier (e.g., "vllm", "together", "deepseek").
// baseURL is the API base (e.g., "https://api.together.xyz/v1").
// apiKey is the authentication key.
// modelID is the model identifier (e.g., "meta-llama/Llama-3.1-70B-Instruct").
func NewOpenAICompatible(name, baseURL, apiKey, modelID string) *OpenAICompatible {
	return NewOpenAICompatibleWithConfig(name, ProviderConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   modelID,
	})
}

// NewOpenAICompatibleWithConfig creates an OpenAI-compatible provider with full config.
func NewOpenAICompatibleWithConfig(name string, cfg ProviderConfig) *OpenAICompatible {
	headers := map[string]string{}
	if cfg.APIKey != "" {
		headers["Authorization"] = "Bearer " + cfg.APIKey
	}
	return &OpenAICompatible{
		config:       cfg,
		providerName: name,
		http:         newHTTPClient(cfg.BaseURL, cfg.TimeoutSec, headers),
	}
}

func (c *OpenAICompatible) Name() string  { return c.providerName }
func (c *OpenAICompatible) Model() string { return c.config.Model }

func (c *OpenAICompatible) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body := buildOpenAIRequestBody(req, c.config.Model, false)

	resp, err := c.http.post(ctx, "/chat/completions", body)
	if err != nil {
		return nil, fmt.Errorf("%s chat: %w", c.providerName, err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s chat: %s", c.providerName, readErrorBody(resp))
	}

	var oaiResp openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("%s chat decode: %w", c.providerName, err)
	}
	return convertOpenAIResponse(&oaiResp), nil
}

func (c *OpenAICompatible) StreamChat(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error) {
	body := buildOpenAIRequestBody(req, c.config.Model, true)

	resp, err := c.http.post(ctx, "/chat/completions", body)
	if err != nil {
		return nil, fmt.Errorf("%s stream: %w", c.providerName, err)
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := readErrorBody(resp)
		resp.Body.Close()
		return nil, fmt.Errorf("%s stream: %s", c.providerName, errMsg)
	}

	ch := make(chan *ChatResponse, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		readOpenAISSEStream(resp, ch)
	}()
	return ch, nil
}

// Convenience constructors for popular OpenAI-compatible providers.

// NewTogether creates a provider for Together AI.
func NewTogether(apiKey, modelID string) *OpenAICompatible {
	return NewOpenAICompatible("together", "https://api.together.xyz/v1", apiKey, modelID)
}

// NewGroq creates a provider for Groq.
func NewGroq(apiKey, modelID string) *OpenAICompatible {
	return NewOpenAICompatible("groq", "https://api.groq.com/openai/v1", apiKey, modelID)
}

// NewDeepSeek creates a provider for DeepSeek.
func NewDeepSeek(apiKey, modelID string) *OpenAICompatible {
	return NewOpenAICompatible("deepseek", "https://api.deepseek.com/v1", apiKey, modelID)
}

// NewOpenRouter creates a provider for OpenRouter.
func NewOpenRouter(apiKey, modelID string) *OpenAICompatible {
	return NewOpenAICompatible("openrouter", "https://openrouter.ai/api/v1", apiKey, modelID)
}

// NewFireworks creates a provider for Fireworks AI.
func NewFireworks(apiKey, modelID string) *OpenAICompatible {
	return NewOpenAICompatible("fireworks", "https://api.fireworks.ai/inference/v1", apiKey, modelID)
}

// NewPerplexity creates a provider for Perplexity.
func NewPerplexity(apiKey, modelID string) *OpenAICompatible {
	return NewOpenAICompatible("perplexity", "https://api.perplexity.ai", apiKey, modelID)
}

// NewAnyscale creates a provider for Anyscale Endpoints.
func NewAnyscale(apiKey, modelID string) *OpenAICompatible {
	return NewOpenAICompatible("anyscale", "https://api.endpoints.anyscale.com/v1", apiKey, modelID)
}
