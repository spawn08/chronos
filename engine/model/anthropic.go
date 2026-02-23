package model

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Anthropic implements Provider for Claude models via the Anthropic Messages API.
type Anthropic struct {
	config ProviderConfig
	http   *httpClient
}

// NewAnthropic creates a new Anthropic provider with the given API key.
func NewAnthropic(apiKey string) *Anthropic {
	return NewAnthropicWithConfig(ProviderConfig{
		APIKey:  apiKey,
		BaseURL: "https://api.anthropic.com",
		Model:   "claude-sonnet-4-20250514",
	})
}

// NewAnthropicWithConfig creates an Anthropic provider with full configuration.
func NewAnthropicWithConfig(cfg ProviderConfig) *Anthropic {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.anthropic.com"
	}
	if cfg.Model == "" {
		cfg.Model = "claude-sonnet-4-20250514"
	}
	headers := map[string]string{
		"x-api-key":         cfg.APIKey,
		"anthropic-version": "2023-06-01",
	}
	return &Anthropic{
		config: cfg,
		http:   newHTTPClient(cfg.BaseURL, cfg.TimeoutSec, headers),
	}
}

func (a *Anthropic) Name() string  { return "anthropic" }
func (a *Anthropic) Model() string { return a.config.Model }

func (a *Anthropic) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body := a.buildRequestBody(req, false)

	resp, err := a.http.post(ctx, "/v1/messages", body)
	if err != nil {
		return nil, fmt.Errorf("anthropic chat: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic chat: %s", readErrorBody(resp))
	}

	var raw anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("anthropic chat decode: %w", err)
	}
	return a.convertResponse(&raw), nil
}

func (a *Anthropic) StreamChat(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error) {
	body := a.buildRequestBody(req, true)

	resp, err := a.http.post(ctx, "/v1/messages", body)
	if err != nil {
		return nil, fmt.Errorf("anthropic stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := readErrorBody(resp)
		resp.Body.Close()
		return nil, fmt.Errorf("anthropic stream: %s", errMsg)
	}

	ch := make(chan *ChatResponse, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		a.readSSEStream(resp, ch)
	}()
	return ch, nil
}

func (a *Anthropic) buildRequestBody(req *ChatRequest, stream bool) map[string]any {
	modelID := req.Model
	if modelID == "" {
		modelID = a.config.Model
	}

	var system string
	messages := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			system = m.Content
			continue
		}
		msg := map[string]any{"role": m.Role}

		if m.Role == RoleTool {
			msg["role"] = RoleUser
			msg["content"] = []map[string]any{{
				"type":         "tool_result",
				"tool_use_id":  m.ToolCallID,
				"content":      m.Content,
			}}
		} else if len(m.ToolCalls) > 0 {
			content := make([]map[string]any, 0, len(m.ToolCalls)+1)
			if m.Content != "" {
				content = append(content, map[string]any{"type": "text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				var args any
				_ = json.Unmarshal([]byte(tc.Arguments), &args)
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": args,
				})
			}
			msg["content"] = content
		} else {
			msg["content"] = m.Content
		}
		messages = append(messages, msg)
	}

	body := map[string]any{
		"model":      modelID,
		"messages":   messages,
		"max_tokens": 4096,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if system != "" {
		body["system"] = system
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		body["top_p"] = req.TopP
	}
	if len(req.Stop) > 0 {
		body["stop_sequences"] = req.Stop
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, len(req.Tools))
		for i, t := range req.Tools {
			tools[i] = map[string]any{
				"name":         t.Function.Name,
				"description":  t.Function.Description,
				"input_schema": t.Function.Parameters,
			}
		}
		body["tools"] = tools
	}
	if stream {
		body["stream"] = true
	}
	return body
}

func (a *Anthropic) convertResponse(raw *anthropicResponse) *ChatResponse {
	cr := &ChatResponse{
		ID:   raw.ID,
		Role: RoleAssistant,
		Usage: Usage{
			PromptTokens:     raw.Usage.InputTokens,
			CompletionTokens: raw.Usage.OutputTokens,
		},
	}

	var textParts []string
	for _, block := range raw.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			argsJSON, _ := json.Marshal(block.Input)
			cr.ToolCalls = append(cr.ToolCalls, ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: string(argsJSON),
			})
		}
	}
	cr.Content = strings.Join(textParts, "")

	switch raw.StopReason {
	case "end_turn", "stop_sequence":
		cr.StopReason = StopReasonEnd
	case "max_tokens":
		cr.StopReason = StopReasonMaxTokens
	case "tool_use":
		cr.StopReason = StopReasonToolCall
	default:
		cr.StopReason = StopReasonEnd
	}
	return cr
}

func (a *Anthropic) readSSEStream(resp *http.Response, ch chan<- *ChatResponse) {
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
			ContentBlock struct {
				Type  string `json:"type"`
				ID    string `json:"id"`
				Name  string `json:"name"`
				Input any    `json:"input"`
			} `json:"content_block"`
			Index int `json:"index"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content_block_delta":
			if event.Delta.Type == "text_delta" {
				ch <- &ChatResponse{
					Content: event.Delta.Text,
					Role:    RoleAssistant,
					Delta:   true,
				}
			}
		case "message_stop":
			return
		}
	}
}

type anthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type  string `json:"type"`
		Text  string `json:"text,omitempty"`
		ID    string `json:"id,omitempty"`
		Name  string `json:"name,omitempty"`
		Input any    `json:"input,omitempty"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}
