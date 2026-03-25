package hooks

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCacheHook_After_IgnoresNonModelAfter_Boost(t *testing.T) {
	h := NewCacheHook(time.Minute)
	err := h.After(context.Background(), &Event{
		Type:   EventToolCallAfter,
		Name:   "t",
		Input:  map[string]string{"x": "y"},
		Output: "out",
	})
	if err != nil {
		t.Fatalf("After: %v", err)
	}
}

func TestCacheHook_After_SkipsWhenErrorOrNilOutput_Boost(t *testing.T) {
	h := NewCacheHook(time.Minute)
	in := map[string]string{"q": "1"}

	err := h.After(context.Background(), &Event{
		Type:   EventModelCallAfter,
		Name:   "m",
		Input:  in,
		Output: nil,
	})
	if err != nil {
		t.Fatal(err)
	}

	err = h.After(context.Background(), &Event{
		Type:   EventModelCallAfter,
		Name:   "m",
		Input:  in,
		Output: "ok",
		Error:  errors.New("fail"),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCacheHook_After_SkipsWhenCacheHitMetadata_Boost(t *testing.T) {
	h := NewCacheHook(time.Minute)
	err := h.After(context.Background(), &Event{
		Type:     EventModelCallAfter,
		Name:     "m",
		Input:    map[string]string{"a": "b"},
		Output:   "cached",
		Metadata: map[string]any{"cache_hit": true},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCacheHook_After_EvictsOldestAtMaxEntries_Boost(t *testing.T) {
	h := NewCacheHook(time.Hour)
	h.MaxEntries = 2

	for i := 1; i <= 3; i++ {
		err := h.After(context.Background(), &Event{
			Type:   EventModelCallAfter,
			Name:   "model",
			Input:  map[string]int{"i": i},
			Output: i,
		})
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}

	h.mu.RLock()
	n := len(h.cache)
	h.mu.RUnlock()
	if n != 2 {
		t.Errorf("cache size = %d, want 2 after eviction", n)
	}
}

type streamMarkedInputBoost struct{}

func (streamMarkedInputBoost) IsStream() bool { return true }

func TestCacheHook_Before_SkipsStreamingInput_Boost(t *testing.T) {
	h := NewCacheHook(time.Minute)
	evt := &Event{
		Type:  EventModelCallBefore,
		Name:  "m",
		Input: streamMarkedInputBoost{},
	}
	if err := h.Before(context.Background(), evt); err != nil {
		t.Fatal(err)
	}
	if evt.Metadata != nil {
		t.Errorf("expected no metadata for streaming skip, got %v", evt.Metadata)
	}
}
