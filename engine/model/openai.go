package model

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// OpenAI implements Provider for OpenAI chat completion endpoints.
// Works with GPT-4, GPT-4o, GPT-3.5-turbo, o1, o3, and any OpenAI-compatible API.
type OpenAI struct {
	config ProviderConfig
	http   *httpClient
}

// NewOpenAI creates a new OpenAI provider with the given API key.
func NewOpenAI(apiKey string) *OpenAI {
	return NewOpenAIWithConfig(ProviderConfig{
		APIKey:  apiKey,
		BaseURL: "https://api.openai.com/v1",
		Model:   "gpt-4o",
	})
}

// NewOpenAIWithConfig creates an OpenAI provider with full configuration.
func NewOpenAIWithConfig(cfg ProviderConfig) *OpenAI {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4o"
	}
	headers := map[string]string{
		"Authorization": "Bearer " + cfg.APIKey,
	}
	if cfg.OrgID != "" {
		headers["OpenAI-Organization"] = cfg.OrgID
	}
	return &OpenAI{
		config: cfg,
		http:   newHTTPClient(cfg.BaseURL, cfg.TimeoutSec, headers),
	}
}

func (o *OpenAI) Name() string  { return "openai" }
func (o *OpenAI) Model() string { return o.config.Model }

func (o *OpenAI) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	body := buildOpenAIRequestBody(req, o.config.Model, false)

	resp, err := o.http.post(ctx, "/chat/completions", body)
	if err != nil {
		return nil, fmt.Errorf("openai chat: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai chat: %s", readErrorBody(resp))
	}

	var oaiResp openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("openai chat decode: %w", err)
	}

	return convertOpenAIResponse(&oaiResp), nil
}

func (o *OpenAI) StreamChat(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error) {
	body := buildOpenAIRequestBody(req, o.config.Model, true)

	resp, err := o.http.post(ctx, "/chat/completions", body)
	if err != nil {
		return nil, fmt.Errorf("openai stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := readErrorBody(resp)
		resp.Body.Close()
		return nil, fmt.Errorf("openai stream: %s", errMsg)
	}

	ch := make(chan *ChatResponse, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		readOpenAISSEStream(resp, ch)
	}()
	return ch, nil
}

// buildOpenAIRequestBody converts a ChatRequest into the OpenAI API JSON body.
// Shared by OpenAI, AzureOpenAI, and OpenAICompatible providers.
func buildOpenAIRequestBody(req *ChatRequest, defaultModel string, stream bool) map[string]any {
	modelID := req.Model
	if modelID == "" {
		modelID = defaultModel
	}

	messages := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		msg := map[string]any{"role": m.Role, "content": m.Content}
		if m.Name != "" {
			msg["name"] = m.Name
		}
		if m.ToolCallID != "" {
			msg["tool_call_id"] = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			tcs := make([]map[string]any, len(m.ToolCalls))
			for i, tc := range m.ToolCalls {
				tcs[i] = map[string]any{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]string{
						"name":      tc.Name,
						"arguments": tc.Arguments,
					},
				}
			}
			msg["tool_calls"] = tcs
		}
		messages = append(messages, msg)
	}

	body := map[string]any{
		"model":    modelID,
		"messages": messages,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		body["top_p"] = req.TopP
	}
	if len(req.Stop) > 0 {
		body["stop"] = req.Stop
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
	}
	if req.ResponseFormat == "json_object" {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	if stream {
		body["stream"] = true
	}
	return body
}

// convertOpenAIResponse maps the raw API response to a ChatResponse.
// Shared by OpenAI, AzureOpenAI, and OpenAICompatible.
func convertOpenAIResponse(oai *openAIChatResponse) *ChatResponse {
	if len(oai.Choices) == 0 {
		return &ChatResponse{ID: oai.ID, Role: RoleAssistant}
	}
	choice := oai.Choices[0]
	cr := &ChatResponse{
		ID:      oai.ID,
		Content: choice.Message.Content,
		Role:    RoleAssistant,
		Usage: Usage{
			PromptTokens:     oai.Usage.PromptTokens,
			CompletionTokens: oai.Usage.CompletionTokens,
		},
	}
	if len(choice.Message.ToolCalls) > 0 {
		cr.StopReason = StopReasonToolCall
		for _, tc := range choice.Message.ToolCalls {
			cr.ToolCalls = append(cr.ToolCalls, ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
	} else {
		cr.StopReason = mapOpenAIFinishReason(choice.FinishReason)
	}
	return cr
}

func mapOpenAIFinishReason(reason string) StopReason {
	switch reason {
	case "length":
		return StopReasonMaxTokens
	case "content_filter":
		return StopReasonFilter
	case "tool_calls":
		return StopReasonToolCall
	default:
		return StopReasonEnd
	}
}

// readOpenAISSEStream reads SSE events from an OpenAI-compatible streaming response.
func readOpenAISSEStream(resp *http.Response, ch chan<- *ChatResponse) {
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return
		}
		var chunk openAIChatResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		cr := &ChatResponse{
			ID:      chunk.ID,
			Content: delta.Content,
			Role:    RoleAssistant,
			Delta:   true,
		}
		if len(delta.ToolCalls) > 0 {
			for _, tc := range delta.ToolCalls {
				cr.ToolCalls = append(cr.ToolCalls, ToolCall{
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				})
			}
		}
		ch <- cr
	}
}

// openAIChatResponse is the raw OpenAI API response shape.
type openAIChatResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls,omitempty"`
		} `json:"message"`
		Delta struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls,omitempty"`
		} `json:"delta"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}
