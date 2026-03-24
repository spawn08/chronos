package hooks

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestChain_Before_StopsOnError(t *testing.T) {
	h1 := &LoggingHook{}
	h2 := &errorHook{err: errors.New("abort")}
	h3 := &LoggingHook{}

	chain := Chain{h1, h2, h3}
	evt := &Event{Type: EventModelCallBefore, Name: "test"}
	err := chain.Before(context.Background(), evt)

	if err == nil {
		t.Fatal("expected error from chain")
	}
	if len(h1.Events) != 1 {
		t.Error("h1 should have been called")
	}
	if len(h3.Events) != 0 {
		t.Error("h3 should NOT have been called after error")
	}
}

func TestChain_After_ReverseOrder(t *testing.T) {
	var order []string
	h1 := &orderHook{id: "first", order: &order}
	h2 := &orderHook{id: "second", order: &order}
	h3 := &orderHook{id: "third", order: &order}

	chain := Chain{h1, h2, h3}
	evt := &Event{Type: EventModelCallAfter, Name: "test"}
	_ = chain.After(context.Background(), evt)

	if len(order) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(order))
	}
	if order[0] != "third" || order[1] != "second" || order[2] != "first" {
		t.Errorf("expected reverse order [third,second,first], got %v", order)
	}
}

func TestChain_Empty(t *testing.T) {
	chain := Chain{}
	evt := &Event{Type: EventModelCallBefore}
	if err := chain.Before(context.Background(), evt); err != nil {
		t.Errorf("empty chain Before: %v", err)
	}
	if err := chain.After(context.Background(), evt); err != nil {
		t.Errorf("empty chain After: %v", err)
	}
}

func TestLoggingHook(t *testing.T) {
	h := &LoggingHook{}
	ctx := context.Background()

	evt := &Event{Type: EventModelCallBefore, Name: "gpt-4o"}
	_ = h.Before(ctx, evt)

	evt2 := &Event{Type: EventModelCallAfter, Name: "gpt-4o", Output: "response"}
	_ = h.After(ctx, evt2)

	if len(h.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(h.Events))
	}
	if h.Events[0].Type != EventModelCallBefore {
		t.Error("first event should be before")
	}
	if h.Events[1].Type != EventModelCallAfter {
		t.Error("second event should be after")
	}
}

func TestCacheHook_HitAndMiss(t *testing.T) {
	cache := NewCacheHook(5 * time.Minute)
	ctx := context.Background()

	input := map[string]string{"prompt": "hello"}
	evt := &Event{Type: EventModelCallBefore, Name: "model", Input: input, Metadata: make(map[string]any)}
	_ = cache.Before(ctx, evt)

	afterEvt := &Event{Type: EventModelCallAfter, Name: "model", Input: input, Output: "response", Metadata: make(map[string]any)}
	_ = cache.After(ctx, afterEvt)

	hits, misses := cache.Stats()
	if hits != 0 || misses != 1 {
		t.Errorf("first call: hits=%d misses=%d, want 0/1", hits, misses)
	}

	evt2 := &Event{Type: EventModelCallBefore, Name: "model", Input: input, Metadata: make(map[string]any)}
	_ = cache.Before(ctx, evt2)

	hits, misses = cache.Stats()
	if hits != 1 || misses != 1 {
		t.Errorf("second call: hits=%d misses=%d, want 1/1", hits, misses)
	}

	if evt2.Metadata["cache_hit"] != true {
		t.Error("expected cache_hit=true in metadata")
	}
}

func TestCacheHook_Clear(t *testing.T) {
	cache := NewCacheHook(5 * time.Minute)
	ctx := context.Background()

	input := map[string]string{"prompt": "test"}
	_ = cache.Before(ctx, &Event{Type: EventModelCallBefore, Name: "m", Input: input})
	_ = cache.After(ctx, &Event{Type: EventModelCallAfter, Name: "m", Input: input, Output: "r"})

	cache.Clear()
	hits, misses := cache.Stats()
	if hits != 0 || misses != 0 {
		t.Errorf("after clear: hits=%d misses=%d", hits, misses)
	}
}

func TestCacheHook_MaxEntries(t *testing.T) {
	cache := NewCacheHook(5 * time.Minute)
	cache.MaxEntries = 2
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		input := map[string]int{"i": i}
		_ = cache.Before(ctx, &Event{Type: EventModelCallBefore, Name: "m", Input: input})
		_ = cache.After(ctx, &Event{Type: EventModelCallAfter, Name: "m", Input: input, Output: i})
	}

	cache.mu.RLock()
	size := len(cache.cache)
	cache.mu.RUnlock()
	if size > 2 {
		t.Errorf("cache size %d exceeds max 2", size)
	}
}

func TestCacheHook_SkipsOnError(t *testing.T) {
	cache := NewCacheHook(5 * time.Minute)
	ctx := context.Background()

	_ = cache.After(ctx, &Event{
		Type: EventModelCallAfter, Name: "m",
		Input: "x", Output: nil, Error: errors.New("fail"),
	})

	cache.mu.RLock()
	size := len(cache.cache)
	cache.mu.RUnlock()
	if size != 0 {
		t.Error("should not cache error responses")
	}
}

func TestCacheHook_IgnoresNonModelEvents(t *testing.T) {
	cache := NewCacheHook(5 * time.Minute)
	ctx := context.Background()

	evt := &Event{Type: EventToolCallBefore, Name: "tool", Metadata: make(map[string]any)}
	_ = cache.Before(ctx, evt)

	if _, ok := evt.Metadata["cache_hit"]; ok {
		t.Error("should not set cache_hit for non-model events")
	}
}

func TestCostTracker_AccumulatesCost(t *testing.T) {
	tracker := NewCostTracker(map[string]ModelPrice{
		"test-model": {PromptPricePerToken: 0.001, CompletionPricePerToken: 0.002},
	})
	ctx := context.Background()

	_ = tracker.Before(ctx, &Event{Type: EventModelCallBefore, Name: "test-model"})
	_ = tracker.After(ctx, &Event{
		Type: EventModelCallAfter, Name: "test-model",
		Metadata: map[string]any{"prompt_tokens": 100, "completion_tokens": 50},
	})

	report := tracker.GetGlobalCost()
	if report.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d, want 100", report.PromptTokens)
	}
	if report.CompletionTokens != 50 {
		t.Errorf("CompletionTokens = %d, want 50", report.CompletionTokens)
	}
	expectedCost := 100*0.001 + 50*0.002
	if report.TotalCost != expectedCost {
		t.Errorf("TotalCost = %f, want %f", report.TotalCost, expectedCost)
	}
}

func TestCostTracker_BudgetExceeded(t *testing.T) {
	tracker := NewCostTracker(map[string]ModelPrice{
		"m": {PromptPricePerToken: 1.0, CompletionPricePerToken: 1.0},
	})
	tracker.Budget = 10.0
	ctx := context.Background()

	_ = tracker.After(ctx, &Event{
		Type: EventModelCallAfter, Name: "m",
		Metadata: map[string]any{"prompt_tokens": 11, "completion_tokens": 0},
	})

	err := tracker.Before(ctx, &Event{Type: EventModelCallBefore, Name: "m"})
	if err == nil {
		t.Fatal("expected budget exceeded error")
	}
}

func TestCostTracker_SessionTracking(t *testing.T) {
	tracker := NewCostTracker(map[string]ModelPrice{
		"m": {PromptPricePerToken: 0.01, CompletionPricePerToken: 0.01},
	})
	ctx := context.Background()

	_ = tracker.After(ctx, &Event{
		Type: EventModelCallAfter, Name: "m",
		Metadata: map[string]any{
			"prompt_tokens": 10, "completion_tokens": 5, "session_id": "s1",
		},
	})

	sr := tracker.GetSessionCost("s1")
	if sr.TotalTokens != 15 {
		t.Errorf("session tokens = %d, want 15", sr.TotalTokens)
	}

	empty := tracker.GetSessionCost("nonexistent")
	if empty.TotalTokens != 0 {
		t.Error("nonexistent session should have 0 tokens")
	}
}

func TestMetricsHook_TracksModelCalls(t *testing.T) {
	m := NewMetricsHook()
	ctx := context.Background()

	_ = m.Before(ctx, &Event{Type: EventModelCallBefore, Name: "gpt-4o"})
	_ = m.After(ctx, &Event{
		Type: EventModelCallAfter, Name: "gpt-4o",
		Metadata: map[string]any{"prompt_tokens": 100, "completion_tokens": 50},
	})

	metrics := m.GetMetrics()
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	if metrics[0].PromptTokens != 100 {
		t.Errorf("PromptTokens = %d", metrics[0].PromptTokens)
	}
	if metrics[0].Duration < 0 {
		t.Error("duration should be non-negative")
	}
}

func TestMetricsHook_Summary(t *testing.T) {
	m := NewMetricsHook()
	ctx := context.Background()

	_ = m.Before(ctx, &Event{Type: EventModelCallBefore, Name: "m"})
	_ = m.After(ctx, &Event{Type: EventModelCallAfter, Name: "m"})

	_ = m.Before(ctx, &Event{Type: EventToolCallBefore, Name: "calc"})
	_ = m.After(ctx, &Event{Type: EventToolCallAfter, Name: "calc"})

	_ = m.Before(ctx, &Event{Type: EventModelCallBefore, Name: "m"})
	_ = m.After(ctx, &Event{Type: EventModelCallAfter, Name: "m", Error: errors.New("fail")})

	s := m.GetSummary()
	if s.TotalModelCalls != 2 {
		t.Errorf("TotalModelCalls = %d, want 2", s.TotalModelCalls)
	}
	if s.TotalToolCalls != 1 {
		t.Errorf("TotalToolCalls = %d, want 1", s.TotalToolCalls)
	}
	if s.TotalErrors != 1 {
		t.Errorf("TotalErrors = %d, want 1", s.TotalErrors)
	}
}

func TestMetricsHook_Reset(t *testing.T) {
	m := NewMetricsHook()
	ctx := context.Background()
	_ = m.Before(ctx, &Event{Type: EventModelCallBefore, Name: "m"})
	_ = m.After(ctx, &Event{Type: EventModelCallAfter, Name: "m"})

	m.Reset()
	if len(m.GetMetrics()) != 0 {
		t.Error("Reset should clear all metrics")
	}
}

func TestRateLimitHook_AllowsWithinLimit(t *testing.T) {
	h := NewRateLimitHook(1000, 0)
	h.WaitOnLimit = false
	ctx := context.Background()

	err := h.Before(ctx, &Event{Type: EventModelCallBefore, Name: "m"})
	if err != nil {
		t.Errorf("should allow within limit: %v", err)
	}
}

func TestRateLimitHook_IgnoresNonModelEvents(t *testing.T) {
	h := NewRateLimitHook(1, 0)
	h.WaitOnLimit = false
	ctx := context.Background()

	err := h.Before(ctx, &Event{Type: EventToolCallBefore, Name: "tool"})
	if err != nil {
		t.Errorf("should ignore non-model events: %v", err)
	}
}

// --- Test helpers ---

type errorHook struct {
	err error
}

func (h *errorHook) Before(_ context.Context, _ *Event) error { return h.err }
func (h *errorHook) After(_ context.Context, _ *Event) error  { return nil }

type orderHook struct {
	id    string
	order *[]string
}

func (h *orderHook) Before(_ context.Context, _ *Event) error { return nil }
func (h *orderHook) After(_ context.Context, _ *Event) error {
	*h.order = append(*h.order, h.id)
	return nil
}
