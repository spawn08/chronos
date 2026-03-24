package hooks

import (
	"context"
	"testing"
	"time"
)

func TestNewCacheHook(t *testing.T) {
	h := NewCacheHook(time.Minute)
	if h == nil {
		t.Fatal("NewCacheHook returned nil")
	}
	if h.TTL != time.Minute {
		t.Errorf("expected TTL 1m, got %v", h.TTL)
	}
}

func TestCacheHookDefaultTTL(t *testing.T) {
	h := NewCacheHook(0)
	if h.TTL != 5*time.Minute {
		t.Errorf("expected default TTL 5m, got %v", h.TTL)
	}
}

func TestCacheHookMiss(t *testing.T) {
	h := NewCacheHook(time.Minute)
	ctx := context.Background()
	evt := &Event{
		Type:  EventModelCallBefore,
		Name:  "gpt-4o",
		Input: map[string]string{"prompt": "hello"},
	}
	if err := h.Before(ctx, evt); err != nil {
		t.Fatalf("Before returned error: %v", err)
	}
	hits, misses := h.Stats()
	if hits != 0 {
		t.Errorf("expected 0 hits, got %d", hits)
	}
	if misses != 1 {
		t.Errorf("expected 1 miss, got %d", misses)
	}
}

func TestCacheHookStoreAndHit(t *testing.T) {
	h := NewCacheHook(time.Minute)
	ctx := context.Background()
	input := map[string]string{"prompt": "hello"}

	// Simulate a model call after (store result)
	afterEvt := &Event{
		Type:   EventModelCallAfter,
		Name:   "gpt-4o",
		Input:  input,
		Output: "response text",
	}
	if err := h.After(ctx, afterEvt); err != nil {
		t.Fatalf("After returned error: %v", err)
	}

	// Now Before should find it
	beforeEvt := &Event{
		Type:  EventModelCallBefore,
		Name:  "gpt-4o",
		Input: input,
	}
	if err := h.Before(ctx, beforeEvt); err != nil {
		t.Fatalf("Before returned error: %v", err)
	}

	hits, _ := h.Stats()
	if hits != 1 {
		t.Errorf("expected 1 hit, got %d", hits)
	}
	if beforeEvt.Metadata["cache_hit"] != true {
		t.Error("expected cache_hit=true in metadata")
	}
	if beforeEvt.Metadata["cached_response"] != "response text" {
		t.Errorf("cached_response mismatch: %v", beforeEvt.Metadata["cached_response"])
	}
}

func TestCacheHookSkipsNonModelEvents(t *testing.T) {
	h := NewCacheHook(time.Minute)
	ctx := context.Background()
	evt := &Event{Type: EventToolCallBefore, Name: "tool", Input: "data"}
	if err := h.Before(ctx, evt); err != nil {
		t.Fatalf("Before returned error: %v", err)
	}
	hits, misses := h.Stats()
	if hits != 0 || misses != 0 {
		t.Errorf("expected no stats for non-model events, got hits=%d misses=%d", hits, misses)
	}
}

func TestCacheHookSkipsNilInput(t *testing.T) {
	h := NewCacheHook(time.Minute)
	ctx := context.Background()
	evt := &Event{Type: EventModelCallBefore, Name: "gpt-4o", Input: nil}
	if err := h.Before(ctx, evt); err != nil {
		t.Fatalf("Before returned error: %v", err)
	}
	// No miss counted since input is nil
	_, misses := h.Stats()
	if misses != 0 {
		t.Errorf("expected 0 misses for nil input, got %d", misses)
	}
}

func TestCacheHookClear(t *testing.T) {
	h := NewCacheHook(time.Minute)
	ctx := context.Background()
	input := "test input"
	h.After(ctx, &Event{Type: EventModelCallAfter, Name: "m", Input: input, Output: "out"})
	h.Before(ctx, &Event{Type: EventModelCallBefore, Name: "m", Input: input})
	h.Clear()
	hits, misses := h.Stats()
	if hits != 0 || misses != 0 {
		t.Errorf("expected 0 stats after clear, got hits=%d misses=%d", hits, misses)
	}
}

func TestCacheHookMaxEntries(t *testing.T) {
	h := NewCacheHook(time.Minute)
	h.MaxEntries = 2
	ctx := context.Background()

	// Add 3 entries — oldest should be evicted
	for i := 0; i < 3; i++ {
		input := map[string]int{"i": i}
		h.After(ctx, &Event{Type: EventModelCallAfter, Name: "m", Input: input, Output: i})
	}

	h.mu.RLock()
	cacheLen := len(h.cache)
	h.mu.RUnlock()
	if cacheLen > 2 {
		t.Errorf("expected at most 2 cache entries, got %d", cacheLen)
	}
}

func TestCacheHookSkipsErrorResponse(t *testing.T) {
	h := NewCacheHook(time.Minute)
	ctx := context.Background()
	err := &Event{
		Type:   EventModelCallAfter,
		Name:   "m",
		Input:  "query",
		Output: "out",
		Error:  context.DeadlineExceeded,
	}
	h.After(ctx, err)

	h.mu.RLock()
	cacheLen := len(h.cache)
	h.mu.RUnlock()
	if cacheLen != 0 {
		t.Errorf("expected nothing cached on error, got %d entries", cacheLen)
	}
}
