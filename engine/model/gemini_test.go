package model

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func buildGeminiServer(t *testing.T, statusCode int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		fmt.Fprint(w, body)
	}))
}

func TestGemini_NewGemini_Defaults(t *testing.T) {
	p := NewGemini("test-key")
	if p.Name() != "gemini" {
		t.Errorf("Name()=%q, want gemini", p.Name())
	}
	if p.Model() != "gemini-2.0-flash" {
		t.Errorf("Model()=%q, want gemini-2.0-flash", p.Model())
	}
}

func TestGemini_NewGeminiWithConfig_CustomModel(t *testing.T) {
	p := NewGeminiWithConfig(ProviderConfig{
		APIKey: "k",
		Model:  "gemini-1.5-pro",
	})
	if p.Model() != "gemini-1.5-pro" {
		t.Errorf("Model()=%q, want gemini-1.5-pro", p.Model())
	}
}

func TestGemini_Chat_Success(t *testing.T) {
	srv := buildGeminiServer(t, 200, `{
		"candidates": [{
			"content": {"parts": [{"text": "Hello from Gemini!"}], "role": "model"},
			"finishReason": "STOP"
		}],
		"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 4}
	}`)
	defer srv.Close()

	p := NewGeminiWithConfig(ProviderConfig{APIKey: "test-key", BaseURL: srv.URL, Model: "gemini-2.0-flash"})
	resp, err := p.Chat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "Hello from Gemini!" {
		t.Errorf("Content=%q, want 'Hello from Gemini!'", resp.Content)
	}
	if resp.StopReason != StopReasonEnd {
		t.Errorf("StopReason=%q, want end", resp.StopReason)
	}
	if resp.Usage.PromptTokens != 5 {
		t.Errorf("PromptTokens=%d, want 5", resp.Usage.PromptTokens)
	}
}

func TestGemini_Chat_Error(t *testing.T) {
	srv := buildGeminiServer(t, 400, `{"error":{"code":400,"message":"Bad request"}}`)
	defer srv.Close()

	p := NewGeminiWithConfig(ProviderConfig{APIKey: "test-key", BaseURL: srv.URL, Model: "gemini-2.0-flash"})
	_, err := p.Chat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 400")
	}
	if !strings.Contains(err.Error(), "gemini chat") {
		t.Errorf("error should mention gemini chat: %v", err)
	}
}

func TestGemini_Chat_FunctionCall(t *testing.T) {
	srv := buildGeminiServer(t, 200, `{
		"candidates": [{
			"content": {
				"parts": [
					{"functionCall": {"name": "get_weather", "args": {"city": "Tokyo"}}}
				],
				"role": "model"
			},
			"finishReason": "STOP"
		}],
		"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 8}
	}`)
	defer srv.Close()

	p := NewGeminiWithConfig(ProviderConfig{APIKey: "test-key", BaseURL: srv.URL, Model: "gemini-2.0-flash"})
	resp, err := p.Chat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Weather in Tokyo?"}},
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

func TestGemini_Chat_MaxTokens(t *testing.T) {
	srv := buildGeminiServer(t, 200, `{
		"candidates": [{
			"content": {"parts": [{"text": "truncated"}], "role": "model"},
			"finishReason": "MAX_TOKENS"
		}]
	}`)
	defer srv.Close()

	p := NewGeminiWithConfig(ProviderConfig{APIKey: "test-key", BaseURL: srv.URL, Model: "gemini-2.0-flash"})
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

func TestGemini_Chat_SafetyFilter(t *testing.T) {
	srv := buildGeminiServer(t, 200, `{
		"candidates": [{
			"content": {"parts": [], "role": "model"},
			"finishReason": "SAFETY"
		}]
	}`)
	defer srv.Close()

	p := NewGeminiWithConfig(ProviderConfig{APIKey: "test-key", BaseURL: srv.URL, Model: "gemini-2.0-flash"})
	resp, err := p.Chat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "something unsafe"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.StopReason != StopReasonFilter {
		t.Errorf("StopReason=%q, want content_filter", resp.StopReason)
	}
}

func TestGemini_Chat_EmptyCandidates(t *testing.T) {
	srv := buildGeminiServer(t, 200, `{"candidates":[],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":0}}`)
	defer srv.Close()

	p := NewGeminiWithConfig(ProviderConfig{APIKey: "test-key", BaseURL: srv.URL, Model: "gemini-2.0-flash"})
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

func TestGemini_Chat_InvalidJSON(t *testing.T) {
	srv := buildGeminiServer(t, 200, "invalid json!")
	defer srv.Close()

	p := NewGeminiWithConfig(ProviderConfig{APIKey: "test-key", BaseURL: srv.URL, Model: "gemini-2.0-flash"})
	_, err := p.Chat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestGemini_BuildRequestBody_SystemInstruction(t *testing.T) {
	p := NewGemini("test")
	req := &ChatRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: "Be concise."},
			{Role: RoleUser, Content: "Hello"},
		},
	}
	body := p.buildRequestBody(req)
	si, _ := body["systemInstruction"].(map[string]any)
	if si == nil {
		t.Fatal("expected systemInstruction")
	}
	parts, _ := si["parts"].([]map[string]string)
	if len(parts) != 1 || parts[0]["text"] != "Be concise." {
		t.Errorf("unexpected system instruction: %v", si)
	}
}

func TestGemini_BuildRequestBody_ToolMessage(t *testing.T) {
	p := NewGemini("test")
	req := &ChatRequest{
		Messages: []Message{
			{Role: RoleTool, Content: "result", Name: "get_weather"},
		},
	}
	body := p.buildRequestBody(req)
	contents, _ := body["contents"].([]map[string]any)
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	if contents[0]["role"] != "function" {
		t.Errorf("role=%v, want function", contents[0]["role"])
	}
}

func TestGemini_BuildRequestBody_AssistantRole(t *testing.T) {
	p := NewGemini("test")
	req := &ChatRequest{
		Messages: []Message{
			{Role: RoleAssistant, Content: "response"},
		},
	}
	body := p.buildRequestBody(req)
	contents, _ := body["contents"].([]map[string]any)
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	if contents[0]["role"] != "model" {
		t.Errorf("role=%v, want model", contents[0]["role"])
	}
}

func TestGemini_BuildRequestBody_GenConfig(t *testing.T) {
	p := NewGemini("test")
	req := &ChatRequest{
		Messages:       []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens:      256,
		Temperature:    0.8,
		TopP:           0.9,
		Stop:           []string{"END"},
		ResponseFormat: "json_object",
	}
	body := p.buildRequestBody(req)
	genConfig, _ := body["generationConfig"].(map[string]any)
	if genConfig == nil {
		t.Fatal("expected generationConfig")
	}
	if genConfig["maxOutputTokens"] != 256 {
		t.Errorf("maxOutputTokens=%v, want 256", genConfig["maxOutputTokens"])
	}
	if genConfig["responseMimeType"] != "application/json" {
		t.Errorf("responseMimeType=%v", genConfig["responseMimeType"])
	}
}

func TestGemini_BuildRequestBody_WithFunctionDecls(t *testing.T) {
	p := NewGemini("test")
	req := &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "call fn"}},
		Tools: []ToolDefinition{
			{Type: "function", Function: FunctionDef{Name: "fn", Description: "desc", Parameters: map[string]any{"type": "object"}}},
		},
	}
	body := p.buildRequestBody(req)
	tools, _ := body["tools"].([]map[string]any)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tools wrapper, got %d", len(tools))
	}
	decls, _ := tools[0]["functionDeclarations"].([]map[string]any)
	if len(decls) != 1 || decls[0]["name"] != "fn" {
		t.Errorf("unexpected functionDeclarations: %v", tools)
	}
}

func TestGemini_BuildRequestBody_ToolCallInMessage(t *testing.T) {
	p := NewGemini("test")
	req := &ChatRequest{
		Messages: []Message{
			{
				Role:      RoleAssistant,
				ToolCalls: []ToolCall{{ID: "tc1", Name: "fn", Arguments: `{"a":1}`}},
			},
		},
	}
	body := p.buildRequestBody(req)
	contents, _ := body["contents"].([]map[string]any)
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	parts, _ := contents[0]["parts"].([]map[string]any)
	if len(parts) < 2 {
		// text part + functionCall part
		t.Errorf("expected >=2 parts (text + functionCall), got %d: %v", len(parts), parts)
	}
}

func TestGemini_StreamChat_Success(t *testing.T) {
	sseBody := `data: {"candidates":[{"content":{"parts":[{"text":"Hello"}],"role":"model"},"finishReason":"STOP"}]}
data: {"candidates":[{"content":{"parts":[{"text":" Gemini"}],"role":"model"},"finishReason":"STOP"}]}
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody)
	}))
	defer srv.Close()

	p := NewGeminiWithConfig(ProviderConfig{APIKey: "test-key", BaseURL: srv.URL, Model: "gemini-2.0-flash"})
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
	if full != "Hello Gemini" {
		t.Errorf("stream content=%q, want 'Hello Gemini'", full)
	}
}

func TestGemini_StreamChat_Error(t *testing.T) {
	srv := buildGeminiServer(t, 429, `{"error":{"code":429,"message":"Rate limited"}}`)
	defer srv.Close()

	p := NewGeminiWithConfig(ProviderConfig{APIKey: "test-key", BaseURL: srv.URL, Model: "gemini-2.0-flash"})
	_, err := p.StreamChat(t.Context(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 429")
	}
}

func TestGemini_Chat_ModelFromRequest(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}],"role":"model"},"finishReason":"STOP"}]}`)
	}))
	defer srv.Close()

	p := NewGeminiWithConfig(ProviderConfig{APIKey: "mykey", BaseURL: srv.URL, Model: "gemini-2.0-flash"})
	p.Chat(t.Context(), &ChatRequest{
		Model:    "gemini-1.5-pro",
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})

	if !strings.Contains(capturedPath, "gemini-1.5-pro") {
		t.Errorf("expected gemini-1.5-pro in path, got %q", capturedPath)
	}
}
