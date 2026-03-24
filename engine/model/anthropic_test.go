package model

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func buildAnthropicServer(t *testing.T, statusCode int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		fmt.Fprint(w, body)
	}))
}

func TestAnthropic_NewAnthropic_Defaults(t *testing.T) {
	p := NewAnthropic("test-key")
	if p.Name() != "anthropic" {
		t.Errorf("Name()=%q, want anthropic", p.Name())
	}
	if p.Model() != "claude-sonnet-4-20250514" {
		t.Errorf("Model()=%q, want claude-sonnet-4-20250514", p.Model())
	}
}

func TestAnthropic_Chat_Success(t *testing.T) {
	srv := buildAnthropicServer(t, 200, `{
		"id": "msg_01",
		"type": "message",
		"role": "assistant",
		"content": [{"type":"text","text":"Hello from Claude!"}],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 5}
	}`)
	defer srv.Close()

	p := NewAnthropicWithConfig(ProviderConfig{APIKey: "test", BaseURL: srv.URL, Model: "claude-3-opus"})
	resp, err := p.Chat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "Hello from Claude!" {
		t.Errorf("Content=%q, want 'Hello from Claude!'", resp.Content)
	}
	if resp.StopReason != StopReasonEnd {
		t.Errorf("StopReason=%q, want end", resp.StopReason)
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("PromptTokens=%d, want 10", resp.Usage.PromptTokens)
	}
}

func TestAnthropic_Chat_Error(t *testing.T) {
	srv := buildAnthropicServer(t, 401, `{"error":{"type":"authentication_error","message":"Invalid key"}}`)
	defer srv.Close()

	p := NewAnthropicWithConfig(ProviderConfig{APIKey: "bad", BaseURL: srv.URL, Model: "claude-3-opus"})
	_, err := p.Chat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "anthropic chat") {
		t.Errorf("error should mention anthropic chat: %v", err)
	}
}

func TestAnthropic_Chat_ToolUse(t *testing.T) {
	srv := buildAnthropicServer(t, 200, `{
		"id": "msg_02",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type":"tool_use","id":"tu_1","name":"get_weather","input":{"city":"Paris"}}
		],
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 20, "output_tokens": 10}
	}`)
	defer srv.Close()

	p := NewAnthropicWithConfig(ProviderConfig{APIKey: "test", BaseURL: srv.URL, Model: "claude-3-opus"})
	resp, err := p.Chat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Weather in Paris?"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.StopReason != StopReasonToolCall {
		t.Errorf("StopReason=%q, want tool_call", resp.StopReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len=%d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Errorf("ToolCall name=%q, want get_weather", resp.ToolCalls[0].Name)
	}
}

func TestAnthropic_Chat_MaxTokens(t *testing.T) {
	srv := buildAnthropicServer(t, 200, `{
		"id": "msg_03",
		"type": "message",
		"role": "assistant",
		"content": [{"type":"text","text":"truncated"}],
		"stop_reason": "max_tokens",
		"usage": {"input_tokens": 5, "output_tokens": 4096}
	}`)
	defer srv.Close()

	p := NewAnthropicWithConfig(ProviderConfig{APIKey: "test", BaseURL: srv.URL, Model: "claude-3-opus"})
	resp, err := p.Chat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "write a lot"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.StopReason != StopReasonMaxTokens {
		t.Errorf("StopReason=%q, want max_tokens", resp.StopReason)
	}
}

func TestAnthropic_Chat_MixedContent(t *testing.T) {
	// Both text and tool_use in same response
	srv := buildAnthropicServer(t, 200, `{
		"id": "msg_04",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type":"text","text":"I'll help you."},
			{"type":"tool_use","id":"tu_2","name":"search","input":{"query":"go"}}
		],
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 15, "output_tokens": 20}
	}`)
	defer srv.Close()

	p := NewAnthropicWithConfig(ProviderConfig{APIKey: "test", BaseURL: srv.URL, Model: "claude-3-opus"})
	resp, err := p.Chat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "search"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "I'll help you." {
		t.Errorf("Content=%q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 {
		t.Errorf("ToolCalls len=%d, want 1", len(resp.ToolCalls))
	}
}

func TestAnthropic_BuildRequestBody_SystemMessage(t *testing.T) {
	p := NewAnthropic("test")
	req := &ChatRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: "You are helpful."},
			{Role: RoleUser, Content: "Hello"},
		},
	}
	body := p.buildRequestBody(req, false)
	if body["system"] != "You are helpful." {
		t.Errorf("system=%v", body["system"])
	}
	msgs, _ := body["messages"].([]map[string]any)
	// System messages are skipped from messages
	if len(msgs) != 1 {
		t.Errorf("expected 1 message (system excluded), got %d", len(msgs))
	}
}

func TestAnthropic_BuildRequestBody_ToolResult(t *testing.T) {
	p := NewAnthropic("test")
	req := &ChatRequest{
		Messages: []Message{
			{Role: RoleTool, Content: "result", ToolCallID: "tc1"},
		},
	}
	body := p.buildRequestBody(req, false)
	msgs, _ := body["messages"].([]map[string]any)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	// Tool result should have role=user with tool_result content
	if msgs[0]["role"] != "user" {
		t.Errorf("role=%v, want user", msgs[0]["role"])
	}
}

func TestAnthropic_BuildRequestBody_ToolCalls(t *testing.T) {
	p := NewAnthropic("test")
	req := &ChatRequest{
		Messages: []Message{
			{
				Role:    RoleAssistant,
				Content: "calling tool",
				ToolCalls: []ToolCall{
					{ID: "tc1", Name: "fn", Arguments: `{"x":1}`},
				},
			},
		},
	}
	body := p.buildRequestBody(req, false)
	msgs, _ := body["messages"].([]map[string]any)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	// Content should be a list with text and tool_use
	content, _ := msgs[0]["content"].([]map[string]any)
	if len(content) < 2 {
		t.Errorf("expected >=2 content blocks, got %d", len(content))
	}
}

func TestAnthropic_BuildRequestBody_WithTools(t *testing.T) {
	p := NewAnthropic("test")
	req := &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []ToolDefinition{
			{Type: "function", Function: FunctionDef{Name: "fn", Description: "test", Parameters: map[string]any{"type": "object"}}},
		},
	}
	body := p.buildRequestBody(req, false)
	tools, _ := body["tools"].([]map[string]any)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0]["name"] != "fn" {
		t.Errorf("tool name=%v, want fn", tools[0]["name"])
	}
	if tools[0]["input_schema"] == nil {
		t.Error("expected input_schema to be set")
	}
}

func TestAnthropic_StreamChat_Success(t *testing.T) {
	sseBody := `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" Claude"}}
data: {"type":"message_stop"}
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody)
	}))
	defer srv.Close()

	p := NewAnthropicWithConfig(ProviderConfig{APIKey: "test", BaseURL: srv.URL, Model: "claude-3-opus"})
	ch, err := p.StreamChat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	var chunks []string
	for cr := range ch {
		if cr.Delta {
			chunks = append(chunks, cr.Content)
		}
	}
	full := strings.Join(chunks, "")
	if full != "Hello Claude" {
		t.Errorf("stream content=%q, want 'Hello Claude'", full)
	}
}

func TestAnthropic_StreamChat_Error(t *testing.T) {
	srv := buildAnthropicServer(t, 500, `{"error":"server error"}`)
	defer srv.Close()

	p := NewAnthropicWithConfig(ProviderConfig{APIKey: "test", BaseURL: srv.URL, Model: "claude-3-opus"})
	_, err := p.StreamChat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestAnthropic_Chat_InvalidJSON(t *testing.T) {
	srv := buildAnthropicServer(t, 200, "this is not json")
	defer srv.Close()

	p := NewAnthropicWithConfig(ProviderConfig{APIKey: "test", BaseURL: srv.URL, Model: "claude-3-opus"})
	_, err := p.Chat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestAnthropic_ConvertResponse_StopSequence(t *testing.T) {
	p := NewAnthropic("test")
	raw := &anthropicResponse{
		ID:         "msg_05",
		StopReason: "stop_sequence",
		Content:    []struct {
			Type  string `json:"type"`
			Text  string `json:"text,omitempty"`
			ID    string `json:"id,omitempty"`
			Name  string `json:"name,omitempty"`
			Input any    `json:"input,omitempty"`
		}{
			{Type: "text", Text: "done."},
		},
	}
	cr := p.convertResponse(raw)
	if cr.StopReason != StopReasonEnd {
		t.Errorf("StopReason=%q, want end", cr.StopReason)
	}
	if cr.Content != "done." {
		t.Errorf("Content=%q, want done.", cr.Content)
	}
}

func TestAnthropic_BuildRequestBody_AllParams(t *testing.T) {
	p := NewAnthropic("test")
	req := &ChatRequest{
		MaxTokens:   512,
		Temperature: 0.5,
		TopP:        0.8,
		Stop:        []string{"END"},
		Messages:    []Message{{Role: RoleUser, Content: "hi"}},
	}
	body := p.buildRequestBody(req, true)

	if body["max_tokens"] != 512 {
		t.Errorf("max_tokens=%v, want 512", body["max_tokens"])
	}
	if body["temperature"] != 0.5 {
		t.Errorf("temperature=%v", body["temperature"])
	}
	if body["stream"] != true {
		t.Errorf("stream=%v, want true", body["stream"])
	}
}
