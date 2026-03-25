package hooks

import (
	"context"
	"testing"
)

func TestRateLimitHook_WaitOnLimitFalse_Max(t *testing.T) {
	h := NewRateLimitHook(1, 0)
	h.WaitOnLimit = false
	ctx := context.Background()
	if err := h.Before(ctx, &Event{Type: EventModelCallBefore, Name: "m"}); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := h.Before(ctx, &Event{Type: EventModelCallBefore, Name: "m"}); err == nil {
		t.Fatal("expected immediate error when wait disabled and bucket empty")
	}
}

func TestRateLimitHook_BeforeIgnoresNonModelBefore_Max(t *testing.T) {
	h := NewRateLimitHook(1, 0)
	if err := h.Before(context.Background(), &Event{Type: EventModelCallAfter, Name: "m"}); err != nil {
		t.Fatal(err)
	}
}

func TestRateLimitHook_AfterTokenBucketConsume_Max(t *testing.T) {
	h := NewRateLimitHook(0, 100)
	_ = h.After(context.Background(), &Event{
		Type:     EventModelCallAfter,
		Name:     "m",
		Metadata: map[string]any{"prompt_tokens": 5},
	})
}
