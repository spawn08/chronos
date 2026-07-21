package hooks

import (
	"context"
	"testing"
	"time"
)

// noopHook is a zero-cost Hook used to isolate the Chain dispatch overhead from
// any per-hook work.
type noopHook struct{}

func (noopHook) Before(context.Context, *Event) error { return nil }
func (noopHook) After(context.Context, *Event) error  { return nil }

// BenchmarkChainBefore measures the fan-through cost of Chain.Before across a
// realistic hook stack depth. This is on the hot path of every node/model/tool
// event.
func BenchmarkChainBefore(b *testing.B) {
	chain := Chain{noopHook{}, noopHook{}, noopHook{}, noopHook{}, noopHook{}}
	ctx := context.Background()
	evt := &Event{Type: EventModelCallBefore, Name: "m"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := chain.Before(ctx, evt); err != nil {
			b.Fatalf("before: %v", err)
		}
	}
}

// BenchmarkChainBeforeAfter measures a full before+after round-trip through the
// chain (After unwinds in reverse), the per-event steady-state cost.
func BenchmarkChainBeforeAfter(b *testing.B) {
	chain := Chain{noopHook{}, noopHook{}, noopHook{}, noopHook{}, noopHook{}}
	ctx := context.Background()
	evt := &Event{Type: EventModelCallAfter, Name: "m"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := chain.Before(ctx, evt); err != nil {
			b.Fatalf("before: %v", err)
		}
		if err := chain.After(ctx, evt); err != nil {
			b.Fatalf("after: %v", err)
		}
	}
}

// BenchmarkCostTrackerAfter measures the token-accounting hot path.
func BenchmarkCostTrackerAfter(b *testing.B) {
	ct := NewCostTracker(map[string]ModelPrice{
		"m": {PromptPricePerToken: 1, CompletionPricePerToken: 1},
	})
	ctx := context.Background()
	evt := &Event{
		Type:     EventModelCallAfter,
		Name:     "m",
		Metadata: map[string]any{"prompt_tokens": 100, "completion_tokens": 50},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ct.After(ctx, evt); err != nil {
			b.Fatalf("after: %v", err)
		}
	}
}

// BenchmarkCacheHookHit measures the cache lookup fast path (a warm hit), which
// short-circuits expensive model calls.
func BenchmarkCacheHookHit(b *testing.B) {
	h := NewCacheHook(time.Hour)
	ctx := context.Background()
	in := map[string]any{"prompt": "hello"}
	// Prime the cache.
	if err := h.After(ctx, &Event{Type: EventModelCallAfter, Name: "m", Input: in, Output: "cached"}); err != nil {
		b.Fatalf("prime: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Before(ctx, &Event{Type: EventModelCallBefore, Name: "m", Input: in})
	}
}

// BenchmarkMetricsHook measures the metrics-recording hot path.
func BenchmarkMetricsHook(b *testing.B) {
	h := NewMetricsHook()
	ctx := context.Background()
	before := &Event{Type: EventModelCallBefore, Name: "m"}
	after := &Event{Type: EventModelCallAfter, Name: "m",
		Metadata: map[string]any{"prompt_tokens": 10, "completion_tokens": 5}}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Before(ctx, before)
		_ = h.After(ctx, after)
	}
}
