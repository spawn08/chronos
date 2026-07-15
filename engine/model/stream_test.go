package model

import (
	"context"
	"errors"
	"testing"
)

func streamOf(items ...*ChatResponse) <-chan *ChatResponse {
	ch := make(chan *ChatResponse, len(items))
	for _, it := range items {
		ch <- it
	}
	close(ch)
	return ch
}

func TestAggregateStream_MergesTextAndUsage(t *testing.T) {
	ch := streamOf(
		&ChatResponse{ID: "resp-1", Content: "Hello", Delta: true, Usage: Usage{PromptTokens: 10}},
		&ChatResponse{Content: " world", Delta: true},
		&ChatResponse{Delta: true, Usage: Usage{CompletionTokens: 4}, StopReason: StopReasonEnd},
	)
	final, err := AggregateStream(context.Background(), ch)
	if err != nil {
		t.Fatalf("AggregateStream: %v", err)
	}
	if final.Content != "Hello world" {
		t.Errorf("content = %q, want 'Hello world'", final.Content)
	}
	if final.ID != "resp-1" {
		t.Errorf("id = %q, want resp-1", final.ID)
	}
	if final.Usage.PromptTokens != 10 || final.Usage.CompletionTokens != 4 {
		t.Errorf("usage = %+v, want {10,4}", final.Usage)
	}
	if final.StopReason != StopReasonEnd {
		t.Errorf("stop reason = %q, want end", final.StopReason)
	}
}

func TestAggregateStream_ReassemblesToolCall(t *testing.T) {
	ch := streamOf(
		&ChatResponse{Delta: true, ToolCalls: []ToolCall{{ID: "call_1", Name: "get_weather"}}},
		&ChatResponse{Delta: true, ToolCalls: []ToolCall{{Arguments: `{"city":`}}},
		&ChatResponse{Delta: true, ToolCalls: []ToolCall{{Arguments: `"paris"}`}}},
	)
	final, err := AggregateStream(context.Background(), ch)
	if err != nil {
		t.Fatalf("AggregateStream: %v", err)
	}
	if len(final.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(final.ToolCalls))
	}
	tc := final.ToolCalls[0]
	if tc.Name != "get_weather" || tc.ID != "call_1" {
		t.Errorf("tool call identity = %+v", tc)
	}
	if tc.Arguments != `{"city":"paris"}` {
		t.Errorf("arguments = %q, want reassembled JSON", tc.Arguments)
	}
	if final.StopReason != StopReasonToolCall {
		t.Errorf("stop reason = %q, want tool_call", final.StopReason)
	}
}

func TestAggregateStream_SurfacesStreamError(t *testing.T) {
	sentinel := errors.New("scanner blew up")
	ch := streamOf(
		&ChatResponse{Content: "partial", Delta: true},
		&ChatResponse{Delta: true, Err: sentinel},
	)
	_, err := AggregateStream(context.Background(), ch)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected surfaced stream error, got %v", err)
	}
}

func TestAggregateStream_ContextCanceled(t *testing.T) {
	ch := make(chan *ChatResponse) // never sends
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := AggregateStream(ctx, ch)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestSendCtx_ReturnsFalseOnCanceledContext(t *testing.T) {
	ch := make(chan *ChatResponse) // unbuffered, no reader
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sendCtx(ctx, ch, &ChatResponse{Content: "x"}) {
		t.Error("sendCtx should return false when context is canceled")
	}
}
