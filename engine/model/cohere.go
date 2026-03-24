package model

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Cohere implements Provider for the Cohere Chat API.
// Supports Command, Command-R, and Command-R+ models.
type Cohere struct {
	config ProviderConfig
	http   *httpClient
}

// NewCohere creates a Cohere provider.
// apiKey is the Cohere API key.
// modelID is the model identifier (e.g., "command-r-plus", "command-r", "command").
func NewCohere(apiKey, modelID string) *Cohere {
	return NewCohereWithConfig(ProviderConfig{
		APIKey:  apiKey,
		BaseURL: "https://api.cohere.ai",
		Model:   modelID,
	})
}

// NewCohereWithConfig creates a Cohere provider with full configuration.
func NewCohereWithConfig(cfg ProviderConfig) *Cohere {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.cohere.ai"
	}
	if cfg.Model == "" {
		cfg.Model = "command-r-plus"
	}
	headers := map[string]string{
		"Authorization": "Bearer " + cfg.APIKey,
	}
	return &Cohere{
		config: cfg,
		http:   newHTTPClient(cfg.BaseURL, cfg.TimeoutSec, headers),
	}
}

func (c *Cohere) Name() string  { return "cohere" }
func (c *Cohere) Model() string { return c.config.Model }

func (c *Cohere) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body := c.buildRequestBody(req, false)

	resp, err := c.http.post(ctx, "/v2/chat", body)
	if err != nil {
		return nil, fmt.Errorf("cohere chat: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cohere chat: %s", readErrorBody(resp))
	}

	var raw cohereResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("cohere chat decode: %w", err)
	}
	return c.convertResponse(&raw), nil
}

func (c *Cohere) StreamChat(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error) {
	body := c.buildRequestBody(req, true)

	resp, err := c.http.post(ctx, "/v2/chat", body)
	if err != nil {
		return nil, fmt.Errorf("cohere stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := readErrorBody(resp)
		resp.Body.Close()
		return nil, fmt.Errorf("cohere stream: %s", errMsg)
	}

	ch := make(chan *ChatResponse, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}
			var event cohereStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				continue
			}
			if event.Delta.Message.Content.Text != "" {
				ch <- &ChatResponse{
					Content: event.Delta.Message.Content.Text,
					Role:    RoleAssistant,
					Delta:   true,
				}
			}
		}
	}()

	return ch, nil
}

func (c *Cohere) buildRequestBody(req *ChatRequest, stream bool) map[string]any {
	messages := make([]map[string]any, 0, len(req.Messages))

	for _, m := range req.Messages {
		role := m.Role
		if role == RoleSystem {
			role = "system"
		}
		messages = append(messages, map[string]any{
			"role":    role,
			"content": m.Content,
		})
	}

	body := map[string]any{
		"model":    c.config.Model,
		"messages": messages,
		"stream":   stream,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}

	if len(req.Tools) > 0 {
		tools := make([]map[string]any, len(req.Tools))
		for i, t := range req.Tools {
			tools[i] = map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Function.Name,
					"description": t.Function.Description,
					"parameters":  t.Function.Parameters,
				},
			}
		}
		body["tools"] = tools
	}

	return body
}

type cohereResponse struct {
	ID      string `json:"id"`
	Message struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		ToolCalls []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"message"`
	FinishReason string `json:"finish_reason"`
	Usage        struct {
		Tokens struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"tokens"`
	} `json:"usage"`
}

type cohereStreamEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Message struct {
			Content struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	} `json:"delta"`
}

func (c *Cohere) convertResponse(raw *cohereResponse) *ChatResponse {
	resp := &ChatResponse{
		ID:   raw.ID,
		Role: RoleAssistant,
		Usage: Usage{
			PromptTokens:     raw.Usage.Tokens.InputTokens,
			CompletionTokens: raw.Usage.Tokens.OutputTokens,
		},
	}

	for _, part := range raw.Message.Content {
		if part.Type == "text" {
			resp.Content += part.Text
		}
	}

	for _, tc := range raw.Message.ToolCalls {
		resp.ToolCalls = append(resp.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}

	switch raw.FinishReason {
	case "COMPLETE":
		resp.StopReason = StopReasonEnd
	case "MAX_TOKENS":
		resp.StopReason = StopReasonMaxTokens
	case "TOOL_CALL":
		resp.StopReason = StopReasonToolCall
	}

	return resp
}
