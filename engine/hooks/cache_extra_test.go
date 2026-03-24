package hooks

import (
	"context"
	"testing"
	"time"
)

type streamInput struct{}

func (streamInput) IsStream() bool { return true }

func TestCacheHook_cacheKey_SkipsStreamingInput(t *testing.T) {
	h := NewCacheHook(time.Minute)
	ctx := context.Background()
	evt := &Event{
		Type:  EventModelCallBefore,
		Name:  "m",
		Input: streamInput{},
	}
	if err := h.Before(ctx, evt); err != nil {
		t.Fatalf("Before: %v", err)
	}
	_, misses := h.Stats()
	if misses != 0 {
		t.Errorf("streaming input should not count as miss, got misses=%d", misses)
	}
}

func TestCacheHook_cacheKey_NonSerializableInput(t *testing.T) {
	h := NewCacheHook(time.Minute)
	ctx := context.Background()
	ch := make(chan int)
	evt := &Event{
		Type:  EventModelCallBefore,
		Name:  "m",
		Input: ch,
	}
	if err := h.Before(ctx, evt); err != nil {
		t.Fatalf("Before: %v", err)
	}
	_, misses := h.Stats()
	if misses != 0 {
		t.Errorf("unmarshalable input should not increment misses, got %d", misses)
	}
}

func TestCacheHook_After_SkipsWhenCacheHitMetadata(t *testing.T) {
	h := NewCacheHook(time.Minute)
	ctx := context.Background()
	in := map[string]string{"q": "x"}
	h.After(ctx, &Event{
		Type:   EventModelCallAfter,
		Name:   "m",
		Input:  in,
		Output: "first",
	})
	h.Before(ctx, &Event{
		Type:  EventModelCallBefore,
		Name:  "m",
		Input: in,
	})
	// Simulate cached response path: After sees cache_hit and must not overwrite cache
	h.After(ctx, &Event{
		Type:   EventModelCallAfter,
		Name:   "m",
		Input:  in,
		Output: "should-not-store",
		Metadata: map[string]any{
			"cache_hit": true,
		},
	})
	h.mu.RLock()
	n := len(h.cache)
	h.mu.RUnlock()
	if n != 1 {
		t.Errorf("expected single cache entry, got %d", n)
	}
}
