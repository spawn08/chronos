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
		Model:   "claude-opus-4-8",
	})
}

// NewAnthropicWithConfig creates an Anthropic provider with full configuration.
func NewAnthropicWithConfig(cfg ProviderConfig) *Anthropic {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.anthropic.com"
	}
	if cfg.Model == "" {
		cfg.Model = "claude-opus-4-8"
	}
	headers := map[string]string{
		"x-api-key":         cfg.APIKey,
		"anthropic-version": "2023-06-01",
	}
	return &Anthropic{
		config: cfg,
		http:   newHTTPClient(cfg.BaseURL, cfg.TimeoutSec, headers, withMaxRetries(cfg.MaxRetries)),
	}
}

func (a *Anthropic) Name() string  { return "anthropic" }
func (a *Anthropic) Model() string { return a.config.Model }

func (a *Anthropic) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if err := validateReasoningToolCompatibility(a.Name(), req); err != nil {
		return nil, fmt.Errorf("anthropic chat: %w", err)
	}
	body := a.buildRequestBody(req, false)

	resp, err := a.http.post(ctx, "/v1/messages", body)
	if err != nil {
		return nil, fmt.Errorf("anthropic chat: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic chat: %w", newAPIError(resp))
	}

	var raw anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("anthropic chat decode: %w", err)
	}
	return a.convertResponse(&raw), nil
}

func (a *Anthropic) StreamChat(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error) {
	if err := validateReasoningToolCompatibility(a.Name(), req); err != nil {
		return nil, fmt.Errorf("anthropic stream: %w", err)
	}
	body := a.buildRequestBody(req, true)

	resp, err := a.http.postStream(ctx, "/v1/messages", body)
	if err != nil {
		return nil, fmt.Errorf("anthropic stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		apiErr := newAPIError(resp)
		resp.Body.Close()
		return nil, fmt.Errorf("anthropic stream: %w", apiErr)
	}

	ch := make(chan *ChatResponse, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		a.readSSEStream(ctx, resp, ch)
	}()
	return ch, nil
}

func (a *Anthropic) buildRequestBody(req *ChatRequest, stream bool) map[string]any {
	modelID := req.Model
	if modelID == "" {
		modelID = a.config.Model
	}

	var systemParts []string
	messages := make([]map[string]any, 0, len(req.Messages))
	for i := range req.Messages {
		if req.Messages[i].Role == RoleSystem {
			if req.Messages[i].Content != "" {
				systemParts = append(systemParts, req.Messages[i].Content)
			}
			continue
		}
		msg := map[string]any{"role": req.Messages[i].Role}

		switch {
		case req.Messages[i].Role == RoleTool:
			msg["role"] = RoleUser
			msg["content"] = []map[string]any{{
				"type":        "tool_result",
				"tool_use_id": req.Messages[i].ToolCallID,
				"content":     req.Messages[i].Content,
			}}
		case len(req.Messages[i].ToolCalls) > 0:
			content := make([]map[string]any, 0, len(req.Messages[i].ToolCalls)+1)
			if req.Messages[i].Content != "" {
				content = append(content, map[string]any{"type": "text", "text": req.Messages[i].Content})
			}
			for _, tc := range req.Messages[i].ToolCalls {
				args := map[string]any{}
				if strings.TrimSpace(tc.Arguments) != "" {
					if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil || args == nil {
						args = map[string]any{}
					}
				}
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": args,
				})
			}
			msg["content"] = content
		default:
			msg["content"] = req.Messages[i].Content
		}
		messages = append(messages, msg)
	}

	if len(messages) == 0 {
		// Anthropic rejects an empty messages array (system is a separate
		// field). Keep the request valid after context trimming.
		messages = append(messages, map[string]any{
			"role":    RoleUser,
			"content": "Continue.",
		})
	}

	body := map[string]any{
		"model":      modelID,
		"messages":   messages,
		"max_tokens": 4096,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if system := strings.Join(systemParts, "\n\n"); system != "" {
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
	if req.Reasoning != nil && req.Reasoning.Enabled {
		budget := req.Reasoning.BudgetTokens
		if budget <= 0 {
			budget = 1024
		}
		body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
		if maxTokens, _ := body["max_tokens"].(int); maxTokens <= budget {
			body["max_tokens"] = budget + 4096
		}
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
	var reasoningParts []string
	for _, block := range raw.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "thinking":
			reasoningParts = append(reasoningParts, block.Thinking)
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
	cr.Reasoning = strings.Join(reasoningParts, "")

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

func (a *Anthropic) readSSEStream(ctx context.Context, resp *http.Response, ch chan<- *ChatResponse) {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

		var event struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			ContentBlock struct {
				Type  string `json:"type"`
				ID    string `json:"id"`
				Name  string `json:"name"`
				Input any    `json:"input"`
			} `json:"content_block"`
			Message struct {
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "message_start":
			// Prompt token usage arrives up front.
			if event.Message.Usage.InputTokens > 0 {
				if !sendCtx(ctx, ch, &ChatResponse{Role: RoleAssistant, Delta: true,
					Usage: Usage{PromptTokens: event.Message.Usage.InputTokens}}) {
					return
				}
			}
		case "content_block_start":
			// A tool_use block begins: emit the tool call identity so callers can
			// start assembling arguments from subsequent input_json_delta events.
			if event.ContentBlock.Type == "tool_use" {
				args := ""
				if event.ContentBlock.Input != nil {
					if b, err := json.Marshal(event.ContentBlock.Input); err == nil && string(b) != "{}" && string(b) != "null" {
						args = string(b)
					}
				}
				if !sendCtx(ctx, ch, &ChatResponse{Role: RoleAssistant, Delta: true,
					StopReason: StopReasonToolCall,
					ToolCalls: []ToolCall{{
						ID:        event.ContentBlock.ID,
						Name:      event.ContentBlock.Name,
						Arguments: args,
					}},
				}) {
					return
				}
			}
		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				if !sendCtx(ctx, ch, &ChatResponse{Content: event.Delta.Text, Role: RoleAssistant, Delta: true}) {
					return
				}
			case "thinking_delta":
				if !sendCtx(ctx, ch, &ChatResponse{Reasoning: event.Delta.Thinking, Role: RoleAssistant, Delta: true}) {
					return
				}
			case "input_json_delta":
				// Streamed fragment of a tool call's JSON arguments.
				if !sendCtx(ctx, ch, &ChatResponse{Role: RoleAssistant, Delta: true,
					StopReason: StopReasonToolCall,
					ToolCalls:  []ToolCall{{Arguments: event.Delta.PartialJSON}},
				}) {
					return
				}
			}
		case "message_delta":
			cr := &ChatResponse{Role: RoleAssistant, Delta: true}
			if event.Usage.OutputTokens > 0 {
				cr.Usage.CompletionTokens = event.Usage.OutputTokens
			}
			if sr := mapAnthropicStopReason(event.Delta.StopReason); sr != "" {
				cr.StopReason = sr
			}
			if cr.Usage.CompletionTokens > 0 || cr.StopReason != "" {
				if !sendCtx(ctx, ch, cr) {
					return
				}
			}
		case "message_stop":
			return
		}
	}
	if err := scanner.Err(); err != nil {
		sendCtx(ctx, ch, &ChatResponse{Role: RoleAssistant, Delta: true, Err: fmt.Errorf("anthropic stream read: %w", err)})
	}
}

// mapAnthropicStopReason converts an Anthropic stop_reason to a StopReason.
// It returns "" when the reason is empty so callers can distinguish "no stop
// reason yet" from an explicit end.
func mapAnthropicStopReason(reason string) StopReason {
	switch reason {
	case "":
		return ""
	case "max_tokens":
		return StopReasonMaxTokens
	case "tool_use":
		return StopReasonToolCall
	default:
		return StopReasonEnd
	}
}

type anthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content []struct {
		Type     string `json:"type"`
		Text     string `json:"text,omitempty"`
		Thinking string `json:"thinking,omitempty"`
		ID       string `json:"id,omitempty"`
		Name     string `json:"name,omitempty"`
		Input    any    `json:"input,omitempty"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}
