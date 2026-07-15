package hooks

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/model"
)

// flakyProvider fails a fixed number of times per call sequence, then succeeds.
type flakyProvider struct {
	mu        sync.Mutex
	failUntil int
	calls     int
}

func (f *flakyProvider) Name() string  { return "flaky" }
func (f *flakyProvider) Model() string { return "flaky" }
func (f *flakyProvider) Chat(_ context.Context, _ *model.ChatRequest) (*model.ChatResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.failUntil {
		return nil, errors.New("transient")
	}
	return &model.ChatResponse{Content: "ok"}, nil
}
func (f *flakyProvider) StreamChat(_ context.Context, _ *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, errors.New("unused")
}

// TestRetryHook_ConcurrentRetriesNoRace exercises the shared Retries counter
// from many goroutines. Run with -race to detect the previously-unguarded
// data race on RetryHook.Retries.
func TestRetryHook_ConcurrentRetriesNoRace(t *testing.T) {
	hook := NewRetryHook(3)
	hook.SleepFn = func(time.Duration) {} // no real delay

	const goroutines = 50
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// The event arrives already failed; the provider succeeds on the
			// first retry, so each goroutine performs exactly one retry.
			prov := &flakyProvider{failUntil: 0}
			evt := &Event{
				Type:  EventModelCallAfter,
				Name:  "flaky",
				Error: errors.New("initial failure"),
				Metadata: map[string]any{
					"provider": model.Provider(prov),
					"request":  &model.ChatRequest{},
				},
			}
			if err := hook.After(context.Background(), evt); err != nil {
				t.Errorf("After: %v", err)
			}
		}()
	}
	wg.Wait()

	// Each goroutine performed exactly one retry (fail once, succeed on retry).
	if got := hook.RetriesCount(); got != goroutines {
		t.Errorf("RetriesCount() = %d, want %d", got, goroutines)
	}
}

// TestRateLimitHook_ParallelWaitersNotSerialized verifies that a waiter blocked
// on capacity does not hold a lock that would serialize other callers. Many
// goroutines acquire request slots concurrently without deadlock or data race.
func TestRateLimitHook_ParallelWaitersNotSerialized(t *testing.T) {
	h := NewRateLimitHook(100000, 0) // high rpm so tokens are available
	ctx := context.Background()

	const goroutines = 100
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 20; j++ {
				if err := h.Before(ctx, &Event{Type: EventModelCallBefore, Name: "m"}); err != nil {
					t.Errorf("Before: %v", err)
					return
				}
				_ = h.After(ctx, &Event{Type: EventModelCallAfter, Name: "m",
					Metadata: map[string]any{"prompt_tokens": 1}})
			}
		}()
	}
	close(start)
	wg.Wait()
}

func TestRateLimitHook_ConcurrentBucketNoRace(t *testing.T) {
	tb := newTokenBucket(1000, time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				tb.tryConsume(1)
				tb.consume(1)
				_ = tb.timeUntilAvailable(1)
			}
		}()
	}
	wg.Wait()
}

// TestCacheHook_ConcurrentAccessNoRace hammers the LRU cache from many
// goroutines and asserts the bound is respected.
func TestCacheHook_ConcurrentAccessNoRace(t *testing.T) {
	h := NewCacheHook(time.Hour)
	h.MaxEntries = 64
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				in := map[string]int{"k": (g*200 + j) % 500}
				h.After(ctx, &Event{Type: EventModelCallAfter, Name: "m", Input: in, Output: j})
				h.Before(ctx, &Event{Type: EventModelCallBefore, Name: "m", Input: in})
			}
		}(i)
	}
	wg.Wait()

	h.mu.RLock()
	n := len(h.cache)
	h.mu.RUnlock()
	if n > h.MaxEntries {
		t.Errorf("cache size = %d, exceeds MaxEntries = %d", n, h.MaxEntries)
	}
}

// TestCostTracker_ConcurrentAccountingNoRace verifies atomic accounting: the
// accumulated totals must equal the sum of all recorded calls with no lost
// updates under concurrency.
func TestCostTracker_ConcurrentAccountingNoRace(t *testing.T) {
	ct := NewCostTracker(map[string]ModelPrice{
		"m": {PromptPricePerToken: 1, CompletionPricePerToken: 1},
	})
	ctx := context.Background()

	const goroutines = 50
	const perG = 100
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				_ = ct.After(ctx, &Event{
					Type: EventModelCallAfter,
					Name: "m",
					Metadata: map[string]any{
						"prompt_tokens":     2,
						"completion_tokens": 3,
					},
				})
			}
		}()
	}
	wg.Wait()

	g := ct.GetGlobalCost()
	wantPrompt := goroutines * perG * 2
	wantCompletion := goroutines * perG * 3
	if g.PromptTokens != wantPrompt || g.CompletionTokens != wantCompletion {
		t.Errorf("tokens = {%d,%d}, want {%d,%d}", g.PromptTokens, g.CompletionTokens, wantPrompt, wantCompletion)
	}
	if g.TotalTokens != wantPrompt+wantCompletion {
		t.Errorf("total tokens = %d, want %d", g.TotalTokens, wantPrompt+wantCompletion)
	}
}
