package hooks

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeSink records the calls made by PrometheusHook.
type fakeSink struct {
	mu        sync.Mutex
	toolCalls []toolCall
	modelCall []modelCall
}

type toolCall struct {
	tenant string
	tool   string
	dur    time.Duration
	isErr  bool
}

type modelCall struct {
	tenant             string
	provider           string
	dur                time.Duration
	prompt, completion int64
	isErr              bool
}

func (s *fakeSink) RecordToolCall(tenant, tool string, d time.Duration, isErr bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolCalls = append(s.toolCalls, toolCall{tenant, tool, d, isErr})
}

func (s *fakeSink) RecordModelCall(tenant, provider string, d time.Duration, prompt, completion int64, isErr bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modelCall = append(s.modelCall, modelCall{tenant, provider, d, prompt, completion, isErr})
}

func TestPrometheusHook_RecordsToolCall(t *testing.T) {
	sink := &fakeSink{}
	h := NewPrometheusHook(sink)
	ctx := WithTenant(context.Background(), "acme")

	evt := &Event{Type: EventToolCallBefore, Name: "calculator"}
	if err := h.Before(ctx, evt); err != nil {
		t.Fatal(err)
	}
	after := &Event{Type: EventToolCallAfter, Name: "calculator"}
	if err := h.After(ctx, after); err != nil {
		t.Fatal(err)
	}

	if len(sink.toolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(sink.toolCalls))
	}
	got := sink.toolCalls[0]
	if got.tenant != "acme" || got.tool != "calculator" || got.isErr {
		t.Errorf("unexpected tool call: %+v", got)
	}
}

func TestPrometheusHook_RecordsModelCallWithTokens(t *testing.T) {
	tests := []struct {
		name           string
		meta           map[string]any
		wantProvider   string
		wantPrompt     int64
		wantCompletion int64
	}{
		{
			name:           "provider and int tokens",
			meta:           map[string]any{"provider": "azure", "prompt_tokens": 100, "completion_tokens": 40},
			wantProvider:   "azure",
			wantPrompt:     100,
			wantCompletion: 40,
		},
		{
			name:           "float64 tokens from json",
			meta:           map[string]any{"provider": "openai", "prompt_tokens": float64(7), "completion_tokens": int64(3)},
			wantProvider:   "openai",
			wantPrompt:     7,
			wantCompletion: 3,
		},
		{
			name:         "falls back to event name when no provider",
			meta:         nil,
			wantProvider: "gpt-4o",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &fakeSink{}
			h := NewPrometheusHook(sink)
			ctx := WithTenant(context.Background(), "t1")

			h.Before(ctx, &Event{Type: EventModelCallBefore, Name: "gpt-4o"})
			h.After(ctx, &Event{Type: EventModelCallAfter, Name: "gpt-4o", Metadata: tt.meta})

			if len(sink.modelCall) != 1 {
				t.Fatalf("model calls = %d, want 1", len(sink.modelCall))
			}
			got := sink.modelCall[0]
			if got.provider != tt.wantProvider {
				t.Errorf("provider = %q, want %q", got.provider, tt.wantProvider)
			}
			if got.prompt != tt.wantPrompt {
				t.Errorf("prompt = %d, want %d", got.prompt, tt.wantPrompt)
			}
			if got.completion != tt.wantCompletion {
				t.Errorf("completion = %d, want %d", got.completion, tt.wantCompletion)
			}
		})
	}
}

func TestPrometheusHook_RecordsError(t *testing.T) {
	sink := &fakeSink{}
	h := NewPrometheusHook(sink)
	ctx := context.Background()

	h.Before(ctx, &Event{Type: EventToolCallBefore, Name: "shell"})
	h.After(ctx, &Event{Type: EventToolCallAfter, Name: "shell", Error: context.DeadlineExceeded})

	if len(sink.toolCalls) != 1 || !sink.toolCalls[0].isErr {
		t.Fatalf("expected one errored tool call, got %+v", sink.toolCalls)
	}
	// empty tenant should be passed through as "" (registry normalizes it).
	if sink.toolCalls[0].tenant != "" {
		t.Errorf("tenant = %q, want empty", sink.toolCalls[0].tenant)
	}
}

func TestPrometheusHook_NilSinkIsNoop(t *testing.T) {
	h := NewPrometheusHook(nil)
	ctx := context.Background()
	if err := h.Before(ctx, &Event{Type: EventToolCallBefore, Name: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := h.After(ctx, &Event{Type: EventToolCallAfter, Name: "x"}); err != nil {
		t.Fatal(err)
	}
}

func TestPrometheusHook_IgnoresUnrelatedEvents(t *testing.T) {
	sink := &fakeSink{}
	h := NewPrometheusHook(sink)
	ctx := context.Background()
	h.After(ctx, &Event{Type: EventNodeAfter, Name: "node1"})
	if len(sink.toolCalls) != 0 || len(sink.modelCall) != 0 {
		t.Error("node events should not produce metrics")
	}
}

func TestPrometheusHook_MeasuresDuration(t *testing.T) {
	sink := &fakeSink{}
	h := NewPrometheusHook(sink)
	ctx := context.Background()
	h.Before(ctx, &Event{Type: EventModelCallBefore, Name: "m"})
	time.Sleep(2 * time.Millisecond)
	h.After(ctx, &Event{Type: EventModelCallAfter, Name: "m"})
	if len(sink.modelCall) != 1 {
		t.Fatalf("want 1 model call")
	}
	if sink.modelCall[0].dur <= 0 {
		t.Errorf("duration = %v, want > 0", sink.modelCall[0].dur)
	}
}

func TestTenantContext(t *testing.T) {
	if TenantFromContext(context.Background()) != "" {
		t.Error("expected empty tenant")
	}
	ctx := WithTenant(context.Background(), "acme")
	if TenantFromContext(ctx) != "acme" {
		t.Errorf("tenant = %q, want acme", TenantFromContext(ctx))
	}
	if TenantFromContext(nil) != "" { //nolint:staticcheck // testing nil safety
		t.Error("nil context should yield empty tenant")
	}
}
