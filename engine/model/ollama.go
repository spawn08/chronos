package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Ollama implements Provider for locally running Ollama models.
// Ollama exposes an OpenAI-compatible API at /v1/chat/completions.
type Ollama struct {
	config ProviderConfig
	http   *httpClient

	windowMu      sync.Mutex
	window        int
	windowChecked time.Time
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

// ollamaWindowRetry spaces lookups that found no served window: /api/ps
// reports one only once the model is loaded, so a later lookup can succeed.
const ollamaWindowRetry = time.Minute

// ollamaWindowTimeout bounds one lookup; it never blocks a request for long.
const ollamaWindowTimeout = 3 * time.Second

// ContextWindow reports the window this server applies to the model. The
// OpenAI-compatible endpoint cannot set num_ctx per request, and Ollama
// silently drops the start of a longer prompt. Only firm evidence counts:
// the model's num_ctx parameter, or the context length /api/ps reports for
// the loaded model. Server-side defaults (OLLAMA_CONTEXT_LENGTH, the app's
// setting, VRAM-based defaults) are not visible from here, so without
// evidence nothing is reported and the configured limit applies.
func (o *Ollama) ContextWindow(ctx context.Context) (int, bool) {
	o.windowMu.Lock()
	defer o.windowMu.Unlock()
	if o.window > 0 {
		return o.window, true
	}
	if !o.windowChecked.IsZero() && time.Since(o.windowChecked) < ollamaWindowRetry {
		return 0, false
	}
	o.windowChecked = time.Now()
	// A cancelled caller must not leave a failed lookup behind.
	lookupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ollamaWindowTimeout)
	defer cancel()
	o.window = o.lookupContextWindow(lookupCtx)
	return o.window, o.window > 0
}

func (o *Ollama) lookupContextWindow(ctx context.Context) int {
	var show struct {
		Parameters string `json:"parameters"`
	}
	if o.getJSON(ctx, http.MethodPost, "/api/show", map[string]any{"model": o.config.Model, "name": o.config.Model}, &show) {
		for _, line := range strings.Split(show.Parameters, "\n") {
			if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "num_ctx" {
				if n, err := strconv.Atoi(fields[1]); err == nil && n > 0 {
					return n
				}
			}
		}
	}
	var ps struct {
		Models []struct {
			Name          string `json:"name"`
			Model         string `json:"model"`
			ContextLength int    `json:"context_length"`
		} `json:"models"`
	}
	if o.getJSON(ctx, http.MethodGet, "/api/ps", nil, &ps) {
		for _, loaded := range ps.Models {
			if (loaded.Name == o.config.Model || loaded.Model == o.config.Model || strings.TrimSuffix(loaded.Name, ":latest") == o.config.Model) && loaded.ContextLength > 0 {
				return loaded.ContextLength
			}
		}
	}
	return 0
}

// getJSON makes one attempt, without the provider's retries, and decodes a
// 200 response into out.
func (o *Ollama) getJSON(ctx context.Context, method, path string, body any, out any) bool {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return false
		}
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(o.config.BaseURL, "/")+path, payload)
	if err != nil {
		return false
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := o.http.client.Do(req)
	if err != nil {
		return false
	}
	defer drainAndClose(resp.Body)
	return resp.StatusCode == http.StatusOK && json.NewDecoder(resp.Body).Decode(out) == nil
}
