package hooks

import (
	"context"
	"fmt"
	"testing"
)

func TestNewMetricsHook(t *testing.T) {
	h := NewMetricsHook()
	if h == nil {
		t.Fatal("NewMetricsHook returned nil")
	}
}

func TestMetricsHookRecordsModelCall(t *testing.T) {
	h := NewMetricsHook()
	ctx := context.Background()

	h.Before(ctx, &Event{Type: EventModelCallBefore, Name: "gpt-4o"})
	h.After(ctx, &Event{
		Type: EventModelCallAfter,
		Name: "gpt-4o",
		Metadata: map[string]any{
			"prompt_tokens":     100,
			"completion_tokens": 50,
		},
	})

	metrics := h.GetMetrics()
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	m := metrics[0]
	if m.Name != "gpt-4o" {
		t.Errorf("expected name gpt-4o, got %s", m.Name)
	}
	if m.PromptTokens != 100 {
		t.Errorf("expected 100 prompt tokens, got %d", m.PromptTokens)
	}
	if m.CompletionTokens != 50 {
		t.Errorf("expected 50 completion tokens, got %d", m.CompletionTokens)
	}
}

func TestMetricsHookRecordsToolCall(t *testing.T) {
	h := NewMetricsHook()
	ctx := context.Background()

	h.Before(ctx, &Event{Type: EventToolCallBefore, Name: "search"})
	h.After(ctx, &Event{Type: EventToolCallAfter, Name: "search"})

	summary := h.GetSummary()
	if summary.TotalToolCalls != 1 {
		t.Errorf("expected 1 tool call, got %d", summary.TotalToolCalls)
	}
}

func TestMetricsHookRecordsErrors(t *testing.T) {
	h := NewMetricsHook()
	ctx := context.Background()

	h.Before(ctx, &Event{Type: EventModelCallBefore, Name: "gpt-4o"})
	h.After(ctx, &Event{Type: EventModelCallAfter, Name: "gpt-4o", Error: fmt.Errorf("fail")})

	summary := h.GetSummary()
	if summary.TotalErrors != 1 {
		t.Errorf("expected 1 error, got %d", summary.TotalErrors)
	}
}

func TestMetricsHookSummaryEmpty(t *testing.T) {
	h := NewMetricsHook()
	s := h.GetSummary()
	if s.TotalModelCalls != 0 || s.TotalToolCalls != 0 {
		t.Error("expected empty summary")
	}
}

func TestMetricsHookReset(t *testing.T) {
	h := NewMetricsHook()
	ctx := context.Background()
	h.Before(ctx, &Event{Type: EventModelCallBefore, Name: "m"})
	h.After(ctx, &Event{Type: EventModelCallAfter, Name: "m"})
	h.Reset()
	if len(h.GetMetrics()) != 0 {
		t.Error("expected empty metrics after reset")
	}
}

func TestMetricsHookSkipsNonCallEvents(t *testing.T) {
	h := NewMetricsHook()
	ctx := context.Background()
	h.Before(ctx, &Event{Type: EventNodeBefore, Name: "node"})
	h.After(ctx, &Event{Type: EventNodeAfter, Name: "node"})
	if len(h.GetMetrics()) != 0 {
		t.Error("should not record non-call events")
	}
}

func TestMetricsHookSummaryTokens(t *testing.T) {
	h := NewMetricsHook()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		h.Before(ctx, &Event{Type: EventModelCallBefore, Name: "m"})
		h.After(ctx, &Event{
			Type: EventModelCallAfter,
			Name: "m",
			Metadata: map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 5,
			},
		})
	}
	s := h.GetSummary()
	if s.TotalModelCalls != 3 {
		t.Errorf("expected 3 model calls, got %d", s.TotalModelCalls)
	}
	if s.TotalPromptTokens != 30 {
		t.Errorf("expected 30 prompt tokens, got %d", s.TotalPromptTokens)
	}
	if s.TotalCompTokens != 15 {
		t.Errorf("expected 15 completion tokens, got %d", s.TotalCompTokens)
	}
}
