package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Ollama implements Provider for locally running Ollama models.
// Ollama exposes an OpenAI-compatible API at /v1/chat/completions.
type Ollama struct {
	config ProviderConfig
	http   *httpClient

	windowOnce sync.Once
	window     int
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
		http:   newHTTPClient(cfg.BaseURL, cfg.TimeoutSec, nil, withMaxRetries(cfg.MaxRetries)),
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
		readOpenAISSEStream(ctx, resp, ch)
	}()
	return ch, nil
}

// ollamaDefaultContext is the window Ollama serves when neither the model
// (num_ctx) nor the server (OLLAMA_CONTEXT_LENGTH) sets one.
const ollamaDefaultContext = 4096

// ContextWindow reports the window this server applies to the model. The
// OpenAI-compatible endpoint cannot set num_ctx per request, and Ollama
// silently drops the start of a longer prompt, so the window comes from the
// model's num_ctx parameter, else OLLAMA_CONTEXT_LENGTH (meaningful when the
// server shares this environment), else Ollama's default, capped at the
// model's trained context length. It is looked up once.
func (o *Ollama) ContextWindow(ctx context.Context) (int, bool) {
	o.windowOnce.Do(func() {
		o.window = o.lookupContextWindow(ctx)
	})
	return o.window, o.window > 0
}

func (o *Ollama) lookupContextWindow(ctx context.Context) int {
	resp, err := o.http.post(ctx, "/api/show", map[string]any{"model": o.config.Model, "name": o.config.Model})
	if err != nil {
		return 0
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return 0
	}
	var show struct {
		Parameters string         `json:"parameters"`
		ModelInfo  map[string]any `json:"model_info"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&show); err != nil {
		return 0
	}
	window := ollamaDefaultContext
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("OLLAMA_CONTEXT_LENGTH"))); err == nil && n > 0 {
		window = n
	}
	for _, line := range strings.Split(show.Parameters, "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "num_ctx" {
			if n, err := strconv.Atoi(fields[1]); err == nil && n > 0 {
				window = n
			}
		}
	}
	for key, value := range show.ModelInfo {
		if trained, ok := value.(float64); ok && strings.HasSuffix(key, ".context_length") && trained > 0 && int(trained) < window {
			window = int(trained)
		}
	}
	return window
}
