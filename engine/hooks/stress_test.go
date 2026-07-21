package hooks

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/model"
)

// TestChain_ConcurrentDispatchNoRace runs a Chain composed of stateful,
// thread-safe hooks (metrics + cost) from many goroutines. It asserts the shared
// hooks accumulate every event with no lost updates and no data race. Run under
// -race.
func TestChain_ConcurrentDispatchNoRace(t *testing.T) {
	const (
		goroutines = 40
		perG       = 100
	)

	metrics := NewMetricsHook()
	cost := NewCostTracker(map[string]ModelPrice{
		"m": {PromptPricePerToken: 1, CompletionPricePerToken: 1},
	})
	chain := Chain{metrics, cost}
	ctx := context.Background()

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				before := &Event{Type: EventModelCallBefore, Name: "m"}
				if err := chain.Before(ctx, before); err != nil {
					t.Errorf("before: %v", err)
					return
				}
				after := &Event{
					Type:     EventModelCallAfter,
					Name:     "m",
					Metadata: map[string]any{"prompt_tokens": 2, "completion_tokens": 3},
				}
				if err := chain.After(ctx, after); err != nil {
					t.Errorf("after: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	// Cost accounting must have no lost updates.
	got := cost.GetGlobalCost()
	wantPrompt := goroutines * perG * 2
	wantComp := goroutines * perG * 3
	if got.PromptTokens != wantPrompt || got.CompletionTokens != wantComp {
		t.Errorf("cost tokens = {%d,%d}, want {%d,%d}", got.PromptTokens, got.CompletionTokens, wantPrompt, wantComp)
	}

	// Metrics must have recorded exactly one CallMetric per After.
	summary := metrics.GetSummary()
	if summary.TotalModelCalls != goroutines*perG {
		t.Errorf("model calls = %d, want %d", summary.TotalModelCalls, goroutines*perG)
	}
}

// TestMetricsHook_ConcurrentBeforeAfterNoRace hammers the pending-start map with
// interleaved Before/After from many goroutines to catch races in the
// start-time bookkeeping.
func TestMetricsHook_ConcurrentBeforeAfterNoRace(t *testing.T) {
	h := NewMetricsHook()
	ctx := context.Background()

	const goroutines = 50
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_ = h.Before(ctx, &Event{Type: EventToolCallBefore, Name: "tool"})
				_ = h.After(ctx, &Event{Type: EventToolCallAfter, Name: "tool"})
				_ = h.GetSummary() // concurrent readers
			}
		}()
	}
	wg.Wait()

	if got := len(h.GetMetrics()); got != goroutines*100 {
		t.Errorf("recorded metrics = %d, want %d", got, goroutines*100)
	}
}

// TestRetryHook_SharedAcrossChainsNoRace exercises a single RetryHook shared by
// many concurrent chains (the pattern that previously raced on the Retries
// counter). Each event fails once then the provider succeeds.
func TestRetryHook_SharedAcrossChainsNoRace(t *testing.T) {
	hook := NewRetryHook(2)
	hook.SleepFn = func(time.Duration) {}

	const goroutines = 60
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			prov := &flakyProvider{failUntil: 0}
			evt := &Event{
				Type:  EventModelCallAfter,
				Name:  "flaky",
				Error: context.DeadlineExceeded,
				Metadata: map[string]any{
					"provider": model.Provider(prov),
					"request":  &model.ChatRequest{},
				},
			}
			_ = hook.After(context.Background(), evt)
		}()
	}
	wg.Wait()

	if got := hook.RetriesCount(); got != goroutines {
		t.Errorf("RetriesCount() = %d, want %d", got, goroutines)
	}
}
