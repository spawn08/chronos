package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func bedrockSuccessBody() string {
	return `{
		"id":"msg-1",
		"content":[{"type":"text","text":"Hello from Bedrock"}],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":5,"output_tokens":10}
	}`
}

func bedrockToolCallBody() string {
	return `{
		"id":"msg-2",
		"content":[{"type":"tool_use","id":"t1","name":"my_tool","input":{"x":1}}],
		"stop_reason":"tool_use",
		"usage":{"input_tokens":5,"output_tokens":5}
	}`
}

func TestNewBedrock_Defaults(t *testing.T) {
	b := NewBedrock("us-east-1", "access-key", "secret-key", "")
	if b.Name() != "bedrock" {
		t.Errorf("Name=%q", b.Name())
	}
	if b.Model() == "" {
		t.Error("Model should have a default")
	}
}

func TestNewBedrockWithConfig_Defaults(t *testing.T) {
	b := NewBedrockWithConfig("us-west-2", ProviderConfig{}, "secret")
	if b.region != "us-west-2" {
		t.Errorf("region=%q", b.region)
	}
	if b.config.Model == "" {
		t.Error("expected default model")
	}
}

func TestBedrock_Chat_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, bedrockSuccessBody())
	}))
	defer srv.Close()

	b := NewBedrockWithConfig("us-east-1", ProviderConfig{
		BaseURL: srv.URL,
		Model:   "anthropic.claude-3-sonnet-20240229-v1:0",
	}, "secret")

	resp, err := b.Chat(context.Background(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "Hello from Bedrock" {
		t.Errorf("Content=%q", resp.Content)
	}
}

func TestBedrock_Chat_Error(t *testing.T) {
	// Build a test server that returns 401
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"unauthorized"}`)
	}))
	defer svr.Close()

	b := NewBedrockWithConfig("us-east-1", ProviderConfig{
		BaseURL: svr.URL,
		Model:   "model",
	}, "secret")

	_, err := b.Chat(context.Background(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
}

func TestBedrock_Chat_WithSystem(t *testing.T) {
	var capturedBody map[string]any
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, bedrockSuccessBody())
	}))
	defer svr.Close()

	b := NewBedrockWithConfig("us-east-1", ProviderConfig{BaseURL: svr.URL, Model: "m"}, "s")
	_, err := b.Chat(context.Background(), &ChatRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: "You are helpful"},
			{Role: RoleUser, Content: "hello"},
		},
		Temperature: 0.5,
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if capturedBody["system"] != "You are helpful" {
		t.Errorf("system prompt not set: %v", capturedBody["system"])
	}
}

func TestBedrock_Chat_ToolUse(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, bedrockToolCallBody())
	}))
	defer svr.Close()

	b := NewBedrockWithConfig("us-east-1", ProviderConfig{BaseURL: svr.URL, Model: "m"}, "s")
	resp, err := b.Chat(context.Background(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "use tool"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.StopReason != StopReasonToolCall {
		t.Errorf("StopReason=%q, want tool_call", resp.StopReason)
	}
}

func TestBedrock_Chat_WithTools(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, bedrockSuccessBody())
	}))
	defer svr.Close()

	b := NewBedrockWithConfig("us-east-1", ProviderConfig{BaseURL: svr.URL, Model: "m"}, "s")
	_, err := b.Chat(context.Background(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
		Tools: []ToolDefinition{
			{Type: "function", Function: FunctionDef{Name: "search", Description: "search the web"}},
		},
	})
	if err != nil {
		t.Fatalf("Chat with tools: %v", err)
	}
}

func TestBedrock_StreamChat(t *testing.T) {
	sseData := "event: content_block_delta\ndata: {\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\nevent: content_block_delta\ndata: {\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\nevent: message_stop\ndata: {}\n\n"
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseData)
	}))
	defer svr.Close()

	b := NewBedrockWithConfig("us-east-1", ProviderConfig{BaseURL: svr.URL, Model: "m"}, "s")
	ch, err := b.StreamChat(context.Background(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	for range ch {
		// drain
	}
}

func TestBedrock_StreamChat_HTTPError(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"server error"}`)
	}))
	defer svr.Close()

	b := NewBedrockWithConfig("us-east-1", ProviderConfig{BaseURL: svr.URL, Model: "m"}, "s")
	_, err := b.StreamChat(context.Background(), &ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestBedrock_BuildRequestBody_NoMaxTokens(t *testing.T) {
	b := NewBedrock("us-east-1", "k", "s", "model")
	body := b.buildRequestBody(&ChatRequest{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if body["max_tokens"] != 4096 {
		t.Errorf("expected default max_tokens=4096, got %v", body["max_tokens"])
	}
}
