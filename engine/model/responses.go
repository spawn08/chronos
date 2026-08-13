package model

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// responsesAPIResponse is the subset of the OpenAI Responses API shape used by
// Chronos. Output items are retained as raw JSON so encrypted reasoning state can
// be passed back unchanged on a stateless tool-calling follow-up.
type responsesAPIResponse struct {
	ID                string             `json:"id"`
	Status            string             `json:"status"`
	Output            []json.RawMessage  `json:"output"`
	Error             *responsesAPIError `json:"error,omitempty"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details,omitempty"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type responsesAPIError struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Param   string `json:"param"`
}

type responsesOutputItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Summary   []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"summary"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func buildResponsesRequestBody(req *ChatRequest, defaultModel string, stream bool) map[string]any {
	modelID := req.Model
	if modelID == "" {
		modelID = defaultModel
	}

	input := make([]any, 0, len(req.Messages))
	for i := range req.Messages {
		m := &req.Messages[i]
		if state, ok := m.ProviderState.([]json.RawMessage); ok && len(state) > 0 {
			for _, item := range state {
				input = append(input, item)
			}
			continue
		}

		switch m.Role {
		case RoleTool:
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": m.ToolCallID,
				"output":  m.Content,
			})
		case RoleAssistant:
			if m.Content != "" {
				input = append(input, map[string]any{"role": RoleAssistant, "content": m.Content})
			}
			for _, tc := range m.ToolCalls {
				input = append(input, map[string]any{
					"type":      "function_call",
					"call_id":   tc.ID,
					"name":      tc.Name,
					"arguments": tc.Arguments,
				})
			}
		default:
			input = append(input, map[string]any{"role": m.Role, "content": m.Content})
		}
	}

	body := map[string]any{
		"model": modelID,
		"input": input,
		// Chronos reconstructs each turn explicitly. Keeping the response
		// stateless avoids server-side retention while encrypted reasoning output
		// below preserves the provider state needed by reasoning models.
		"store": false,
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, tool := range req.Tools {
			tools = append(tools, map[string]any{
				"type":        "function",
				"name":        tool.Function.Name,
				"description": tool.Function.Description,
				"parameters":  tool.Function.Parameters,
			})
		}
		body["tools"] = tools
	}
	if req.MaxTokens > 0 {
		body["max_output_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		body["top_p"] = req.TopP
	}
	if req.ResponseFormat == "json_object" {
		body["text"] = map[string]any{"format": map[string]string{"type": "json_object"}}
	}
	if nativeReasoningEnabled(req.Reasoning) {
		reasoning := map[string]any{}
		if req.Reasoning.Effort != "" {
			reasoning["effort"] = req.Reasoning.Effort
		}
		if req.Reasoning.Summary {
			reasoning["summary"] = "auto"
		}
		body["reasoning"] = reasoning
		body["include"] = []string{"reasoning.encrypted_content"}
	}
	if stream {
		body["stream"] = true
	}
	return body
}

func convertResponsesResponse(raw *responsesAPIResponse) *ChatResponse {
	resp := &ChatResponse{
		ID:   raw.ID,
		Role: RoleAssistant,
		Usage: Usage{
			PromptTokens:     raw.Usage.InputTokens,
			CompletionTokens: raw.Usage.OutputTokens,
		},
		ProviderState: raw.Output,
	}

	var content, reasoning strings.Builder
	for _, rawItem := range raw.Output {
		var item responsesOutputItem
		if err := json.Unmarshal(rawItem, &item); err != nil {
			continue
		}
		switch item.Type {
		case "reasoning":
			for _, part := range item.Summary {
				if part.Text == "" {
					continue
				}
				if reasoning.Len() > 0 {
					reasoning.WriteByte('\n')
				}
				reasoning.WriteString(part.Text)
			}
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" {
					content.WriteString(part.Text)
				}
			}
		case "function_call":
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{
				ID:        item.CallID,
				Name:      item.Name,
				Arguments: item.Arguments,
			})
		}
	}
	resp.Content = content.String()
	resp.Reasoning = reasoning.String()
	switch {
	case len(resp.ToolCalls) > 0:
		resp.StopReason = StopReasonToolCall
	case raw.Status == "incomplete" && raw.IncompleteDetails != nil && raw.IncompleteDetails.Reason == "max_output_tokens":
		resp.StopReason = StopReasonMaxTokens
	default:
		resp.StopReason = StopReasonEnd
	}
	return resp
}

func responsesError(raw *responsesAPIResponse) error {
	if raw == nil || raw.Error == nil {
		return nil
	}
	if raw.Error.Code != "" {
		return fmt.Errorf("responses API %s (%s): %s", raw.Error.Type, raw.Error.Code, raw.Error.Message)
	}
	return fmt.Errorf("responses API %s: %s", raw.Error.Type, raw.Error.Message)
}

// readResponsesSSEStream converts Responses API semantic events into Chronos
// text/reasoning deltas plus one terminal metadata chunk. The completed response
// carries full output items so stateless tool rounds retain encrypted reasoning.
func readResponsesSSEStream(ctx context.Context, resp *http.Response, ch chan<- *ChatResponse) {
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var event struct {
			Type     string                `json:"type"`
			Delta    string                `json:"delta"`
			Response *responsesAPIResponse `json:"response"`
			Error    *responsesAPIError    `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		switch event.Type {
		case "response.output_text.delta":
			if !sendCtx(ctx, ch, &ChatResponse{Role: RoleAssistant, Content: event.Delta, Delta: true}) {
				return
			}
		case "response.reasoning_summary_text.delta":
			if !sendCtx(ctx, ch, &ChatResponse{Role: RoleAssistant, Reasoning: event.Delta, Delta: true}) {
				return
			}
		case "response.completed":
			if err := responsesError(event.Response); err != nil {
				sendCtx(ctx, ch, &ChatResponse{Role: RoleAssistant, Delta: true, Err: err})
				return
			}
			terminal := convertResponsesResponse(event.Response)
			// Text and reasoning were already emitted as deltas. Keep only the
			// terminal tool calls, usage, stop reason, and opaque provider state.
			terminal.Content = ""
			terminal.Reasoning = ""
			terminal.Delta = true
			if !sendCtx(ctx, ch, terminal) {
				return
			}
		case "response.failed", "response.incomplete":
			if err := responsesError(event.Response); err != nil {
				sendCtx(ctx, ch, &ChatResponse{Role: RoleAssistant, Delta: true, Err: err})
			} else {
				sendCtx(ctx, ch, &ChatResponse{Role: RoleAssistant, Delta: true, Err: fmt.Errorf("responses API ended with status %s", event.Type)})
			}
			return
		case "error":
			if event.Error != nil {
				sendCtx(ctx, ch, &ChatResponse{Role: RoleAssistant, Delta: true, Err: fmt.Errorf("responses API %s: %s", event.Error.Type, event.Error.Message)})
			}
			return
		}
	}
	if err := scanner.Err(); err != nil {
		sendCtx(ctx, ch, &ChatResponse{Role: RoleAssistant, Delta: true, Err: fmt.Errorf("responses stream read: %w", err)})
	}
}
