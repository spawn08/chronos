package model

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// buildOpenAIServer creates a test server that returns the given OpenAI-style response.
func buildOpenAIServer(t *testing.T, statusCode int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		fmt.Fprint(w, body)
	}))
}

func TestOpenAI_NewOpenAI_Defaults(t *testing.T) {
	p := NewOpenAI("test-key")
	if p.Name() != "openai" {
		t.Errorf("Name()=%q, want openai", p.Name())
	}
	if p.Model() != "gpt-4o" {
		t.Errorf("Model()=%q, want gpt-4o", p.Model())
	}
}

func TestOpenAI_NewOpenAIWithConfig_CustomModel(t *testing.T) {
	p := NewOpenAIWithConfig(ProviderConfig{
		APIKey: "k",
		Model:  "gpt-4-turbo",
	})
	if p.Model() != "gpt-4-turbo" {
		t.Errorf("Model()=%q, want gpt-4-turbo", p.Model())
	}
}

func TestOpenAI_Chat_Success(t *testing.T) {
	srv := buildOpenAIServer(t, 200, `{
		"id":"chatcmpl-123",
		"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"Hello!"}}],
		"usage":{"prompt_tokens":10,"completion_tokens":5}
	}`)
	defer srv.Close()

	p := NewOpenAIWithConfig(ProviderConfig{APIKey: "test", BaseURL: srv.URL, Model: "gpt-4o"})
	resp, err := p.Chat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("Content=%q, want Hello!", resp.Content)
	}
	if resp.ID != "chatcmpl-123" {
		t.Errorf("ID=%q, want chatcmpl-123", resp.ID)
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("PromptTokens=%d, want 10", resp.Usage.PromptTokens)
	}
	if resp.StopReason != StopReasonEnd {
		t.Errorf("StopReason=%q, want end", resp.StopReason)
	}
}

func TestOpenAI_Chat_Error(t *testing.T) {
	srv := buildOpenAIServer(t, 401, `{"error":{"message":"Invalid API key"}}`)
	defer srv.Close()

	p := NewOpenAIWithConfig(ProviderConfig{APIKey: "bad", BaseURL: srv.URL, Model: "gpt-4o"})
	_, err := p.Chat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "openai chat") {
		t.Errorf("error should mention openai chat: %v", err)
	}
}

func TestOpenAI_Chat_ToolCall(t *testing.T) {
	srv := buildOpenAIServer(t, 200, `{
		"id":"chatcmpl-456",
		"choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"London\"}"}}]}}],
		"usage":{"prompt_tokens":20,"completion_tokens":15}
	}`)
	defer srv.Close()

	p := NewOpenAIWithConfig(ProviderConfig{APIKey: "test", BaseURL: srv.URL, Model: "gpt-4o"})
	resp, err := p.Chat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Weather in London?"}},
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

func TestOpenAI_Chat_EmptyChoices(t *testing.T) {
	srv := buildOpenAIServer(t, 200, `{"id":"chatcmpl-789","choices":[],"usage":{}}`)
	defer srv.Close()

	p := NewOpenAIWithConfig(ProviderConfig{APIKey: "test", BaseURL: srv.URL, Model: "gpt-4o"})
	resp, err := p.Chat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "" {
		t.Errorf("expected empty content, got %q", resp.Content)
	}
}

func TestOpenAI_StreamChat_Success(t *testing.T) {
	sseBody := `data: {"id":"chat-1","choices":[{"delta":{"content":"Hello"}}]}
data: {"id":"chat-1","choices":[{"delta":{"content":" world"}}]}
data: [DONE]
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody)
	}))
	defer srv.Close()

	p := NewOpenAIWithConfig(ProviderConfig{APIKey: "test", BaseURL: srv.URL, Model: "gpt-4o"})
	ch, err := p.StreamChat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}

	var chunks []string
	for cr := range ch {
		chunks = append(chunks, cr.Content)
	}
	full := strings.Join(chunks, "")
	if full != "Hello world" {
		t.Errorf("stream content=%q, want 'Hello world'", full)
	}
}

func TestOpenAI_StreamChat_Error(t *testing.T) {
	srv := buildOpenAIServer(t, 403, `{"error":"forbidden"}`)
	defer srv.Close()

	p := NewOpenAIWithConfig(ProviderConfig{APIKey: "test", BaseURL: srv.URL, Model: "gpt-4o"})
	_, err := p.StreamChat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 403")
	}
}

func TestBuildOpenAIRequestBody_AllFields(t *testing.T) {
	req := &ChatRequest{
		Model:          "gpt-4",
		MaxTokens:      100,
		Temperature:    0.7,
		TopP:           0.9,
		Stop:           []string{"STOP"},
		ResponseFormat: "json_object",
		Messages: []Message{
			{Role: RoleUser, Content: "Hello"},
			{Role: RoleAssistant, Content: "Hi", ToolCalls: []ToolCall{{ID: "t1", Name: "fn", Arguments: "{}"}}},
			{Role: RoleTool, Content: "result", ToolCallID: "t1"},
		},
		Tools: []ToolDefinition{
			{Type: "function", Function: FunctionDef{Name: "fn", Description: "A function"}},
		},
	}
	body := buildOpenAIRequestBody(req, "gpt-4o", true)

	if body["model"] != "gpt-4" {
		t.Errorf("model=%v", body["model"])
	}
	if body["max_tokens"] != 100 {
		t.Errorf("max_tokens=%v", body["max_tokens"])
	}
	if body["temperature"] != 0.7 {
		t.Errorf("temperature=%v", body["temperature"])
	}
	if body["top_p"] != 0.9 {
		t.Errorf("top_p=%v", body["top_p"])
	}
	if body["stream"] != true {
		t.Errorf("stream=%v", body["stream"])
	}
	rf, _ := body["response_format"].(map[string]string)
	if rf["type"] != "json_object" {
		t.Errorf("response_format.type=%v", rf["type"])
	}
}

func TestBuildOpenAIRequestBody_DefaultModel(t *testing.T) {
	req := &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "Hi"}}}
	body := buildOpenAIRequestBody(req, "gpt-default", false)
	if body["model"] != "gpt-default" {
		t.Errorf("should use default model, got %v", body["model"])
	}
}

func TestBuildOpenAIRequestBody_MessageWithName(t *testing.T) {
	req := &ChatRequest{
		Messages: []Message{{Role: RoleTool, Content: "result", Name: "my_tool", ToolCallID: "tc1"}},
	}
	body := buildOpenAIRequestBody(req, "gpt-4o", false)
	msgs, _ := body["messages"].([]map[string]any)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0]["name"] != "my_tool" {
		t.Errorf("name=%v, want my_tool", msgs[0]["name"])
	}
	if msgs[0]["tool_call_id"] != "tc1" {
		t.Errorf("tool_call_id=%v, want tc1", msgs[0]["tool_call_id"])
	}
}

func TestMapOpenAIFinishReason(t *testing.T) {
	tests := []struct {
		in   string
		want StopReason
	}{
		{"stop", StopReasonEnd},
		{"length", StopReasonMaxTokens},
		{"content_filter", StopReasonFilter},
		{"tool_calls", StopReasonToolCall},
		{"unknown", StopReasonEnd},
	}
	for _, tt := range tests {
		got := mapOpenAIFinishReason(tt.in)
		if got != tt.want {
			t.Errorf("mapOpenAIFinishReason(%q)=%q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestConvertOpenAIResponse_WithToolCalls(t *testing.T) {
	raw := &openAIChatResponse{
		ID: "id1",
		Choices: []struct {
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
		}{
			{
				FinishReason: "tool_calls",
				Message: struct {
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
				}{
					Role: "assistant",
					ToolCalls: []struct {
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					}{
						{ID: "call_1", Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{Name: "search", Arguments: `{"q":"test"}`}},
					},
				},
			},
		},
	}

	cr := convertOpenAIResponse(raw)
	if cr.StopReason != StopReasonToolCall {
		t.Errorf("StopReason=%q, want tool_call", cr.StopReason)
	}
	if len(cr.ToolCalls) != 1 || cr.ToolCalls[0].Name != "search" {
		t.Errorf("unexpected tool calls: %+v", cr.ToolCalls)
	}
}

func TestOpenAI_Chat_InvalidJSON(t *testing.T) {
	srv := buildOpenAIServer(t, 200, "not-json")
	defer srv.Close()

	p := NewOpenAIWithConfig(ProviderConfig{APIKey: "test", BaseURL: srv.URL, Model: "gpt-4o"})
	_, err := p.Chat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestOpenAI_WithOrgID(t *testing.T) {
	var capturedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"c1","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer srv.Close()

	p := NewOpenAIWithConfig(ProviderConfig{
		APIKey:  "test",
		BaseURL: srv.URL,
		Model:   "gpt-4o",
		OrgID:   "org-abc",
	})
	p.Chat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "Hi"}}})

	if capturedHeaders.Get("OpenAI-Organization") != "org-abc" {
		t.Errorf("expected OpenAI-Organization header, got: %q", capturedHeaders.Get("OpenAI-Organization"))
	}
}

// Ensure the openAIChatResponse type can be decoded properly (JSON roundtrip).
func TestOpenAIChatResponse_JSONRoundtrip(t *testing.T) {
	raw := `{
		"id": "chatcmpl-xyz",
		"choices": [
			{
				"index": 0,
				"finish_reason": "stop",
				"message": {"role": "assistant", "content": "Hello"},
				"delta": {}
			}
		],
		"usage": {"prompt_tokens": 5, "completion_tokens": 3}
	}`
	var resp openAIChatResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp.ID != "chatcmpl-xyz" {
		t.Errorf("ID=%q", resp.ID)
	}
	if resp.Choices[0].Message.Content != "Hello" {
		t.Errorf("Content=%q", resp.Choices[0].Message.Content)
	}
}
