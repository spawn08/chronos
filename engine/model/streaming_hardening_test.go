package model

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAI_StreamChat_CapturesUsageAndRequestsIncludeUsage(t *testing.T) {
	var gotBody map[string]any
	sse := `data: {"id":"c1","choices":[{"delta":{"content":"Hi"}}]}
data: {"id":"c1","choices":[],"usage":{"prompt_tokens":11,"completion_tokens":3}}
data: [DONE]
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse)
	}))
	defer srv.Close()

	p := NewOpenAIWithConfig(ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "gpt-4o"})
	ch, err := p.StreamChat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	final, err := AggregateStream(context.Background(), ch)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if final.Content != "Hi" {
		t.Errorf("content = %q", final.Content)
	}
	if final.Usage.PromptTokens != 11 || final.Usage.CompletionTokens != 3 {
		t.Errorf("usage = %+v, want {11,3}", final.Usage)
	}
	// stream_options.include_usage must be requested.
	so, ok := gotBody["stream_options"].(map[string]any)
	if !ok || so["include_usage"] != true {
		t.Errorf("stream_options.include_usage not requested: %v", gotBody["stream_options"])
	}
}

func TestAnthropic_StreamChat_ToolUseAndUsage(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":25}}}`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tu_1","name":"lookup"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"go\"}"}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":7}}`,
		`data: {"type":"message_stop"}`,
		"",
	}, "\n\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse)
	}))
	defer srv.Close()

	p := NewAnthropicWithConfig(ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "claude-opus-4-8"})
	ch, err := p.StreamChat(context.Background(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	final, err := AggregateStream(context.Background(), ch)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if len(final.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(final.ToolCalls))
	}
	tc := final.ToolCalls[0]
	if tc.ID != "tu_1" || tc.Name != "lookup" {
		t.Errorf("tool identity = %+v", tc)
	}
	if tc.Arguments != `{"q":"go"}` {
		t.Errorf("arguments = %q, want reassembled JSON", tc.Arguments)
	}
	if final.StopReason != StopReasonToolCall {
		t.Errorf("stop reason = %q, want tool_call", final.StopReason)
	}
	if final.Usage.PromptTokens != 25 || final.Usage.CompletionTokens != 7 {
		t.Errorf("usage = %+v, want {25,7}", final.Usage)
	}
}

// TestOpenAI_StreamChat_DisconnectFreesGoroutine verifies that canceling the
// caller context lets the reader goroutine unwind instead of blocking forever
// on an unread channel (leaking the goroutine/connection).
func TestOpenAI_StreamChat_DisconnectFreesGoroutine(t *testing.T) {
	// Server streams many chunks slowly and stays open.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for i := 0; i < 1000; i++ {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			fmt.Fprintf(w, "data: {\"id\":\"c\",\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
			if fl != nil {
				fl.Flush()
			}
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	p := NewOpenAIWithConfig(ProviderConfig{APIKey: "k", BaseURL: srv.URL, Model: "gpt-4o"})
	ch, err := p.StreamChat(ctx, &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	// Read one chunk then abandon the stream by canceling.
	<-ch
	cancel()
	// The goroutine must eventually close the channel; draining completes rather
	// than hanging (the test times out if the goroutine leaks).
	for range ch { //nolint:revive // intentionally draining until close
	}
}
