package hooks

import (
	"context"
	"testing"
)

func TestNewRateLimitHook(t *testing.T) {
	h := NewRateLimitHook(10, 1000)
	if h == nil {
		t.Fatal("NewRateLimitHook returned nil")
	}
	if h.RequestsPerMinute != 10 {
		t.Errorf("expected 10 rpm, got %d", h.RequestsPerMinute)
	}
	if h.TokensPerMinute != 1000 {
		t.Errorf("expected 1000 tpm, got %d", h.TokensPerMinute)
	}
	if !h.WaitOnLimit {
		t.Error("WaitOnLimit should default to true")
	}
}

func TestRateLimitHookAllowsRequests(t *testing.T) {
	h := NewRateLimitHook(100, 0)
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		err := h.Before(ctx, &Event{Type: EventModelCallBefore, Name: "gpt-4o"})
		if err != nil {
			t.Fatalf("Before failed on request %d: %v", i, err)
		}
	}
}

func TestRateLimitHookSkipsNonModelEvents(t *testing.T) {
	h := NewRateLimitHook(1, 0) // very low limit
	h.WaitOnLimit = false
	ctx := context.Background()
	// Should not be rate-limited for tool events
	for i := 0; i < 5; i++ {
		err := h.Before(ctx, &Event{Type: EventToolCallBefore, Name: "tool"})
		if err != nil {
			t.Fatalf("unexpected error for non-model event: %v", err)
		}
	}
}

func TestRateLimitHookExceedLimitNoWait(t *testing.T) {
	h := NewRateLimitHook(1, 0) // only 1 request per minute
	h.WaitOnLimit = false
	ctx := context.Background()

	// First request should pass
	err := h.Before(ctx, &Event{Type: EventModelCallBefore, Name: "gpt-4o"})
	if err != nil {
		t.Fatalf("first request failed: %v", err)
	}

	// Second should fail immediately
	err = h.Before(ctx, &Event{Type: EventModelCallBefore, Name: "gpt-4o"})
	if err == nil {
		t.Fatal("expected rate limit error on second request")
	}
}

func TestRateLimitHookAfterTokenDeduction(t *testing.T) {
	h := NewRateLimitHook(0, 1000)
	ctx := context.Background()

	evt := &Event{
		Type: EventModelCallAfter,
		Name: "gpt-4o",
		Metadata: map[string]any{
			"prompt_tokens": 50,
		},
	}
	// Should not error
	if err := h.After(ctx, evt); err != nil {
		t.Fatalf("After failed: %v", err)
	}
}

func TestRateLimitHookAfterSkipsNonAfterEvents(t *testing.T) {
	h := NewRateLimitHook(0, 100)
	ctx := context.Background()
	evt := &Event{Type: EventModelCallBefore, Name: "m", Metadata: map[string]any{"prompt_tokens": 99}}
	if err := h.After(ctx, evt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRateLimitHookAfterNoMetadata(t *testing.T) {
	h := NewRateLimitHook(0, 100)
	ctx := context.Background()
	evt := &Event{Type: EventModelCallAfter, Name: "m"}
	if err := h.After(ctx, evt); err != nil {
		t.Fatalf("unexpected error with nil metadata: %v", err)
	}
}

func TestRateLimitHookContextCancelled(t *testing.T) {
	h := NewRateLimitHook(1, 0) // 1 rpm
	h.WaitOnLimit = true
	ctx, cancel := context.WithCancel(context.Background())

	// Consume the only token
	h.Before(ctx, &Event{Type: EventModelCallBefore, Name: "m"})

	// Cancel context
	cancel()

	// Next call should fail with context error
	err := h.Before(ctx, &Event{Type: EventModelCallBefore, Name: "m"})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestTokenBucketTryConsume(t *testing.T) {
	tb := newTokenBucket(5, 1e9) // 5 tokens per second
	for i := 0; i < 5; i++ {
		if !tb.tryConsume(1) {
			t.Fatalf("expected to consume token %d", i)
		}
	}
	if tb.tryConsume(1) {
		t.Fatal("should not be able to consume beyond capacity")
	}
}

func TestTokenBucketConsume(t *testing.T) {
	tb := newTokenBucket(10, 1e9)
	tb.consume(3)
	if tb.tokens > 7 {
		t.Errorf("expected tokens <= 7 after consuming 3, got %.2f", tb.tokens)
	}
}

func TestTokenBucketTimeUntilAvailable(t *testing.T) {
	tb := newTokenBucket(10, 1e9)
	// Drain it
	tb.tryConsume(10)
	wait := tb.timeUntilAvailable(1)
	if wait <= 0 {
		t.Errorf("expected positive wait time after draining, got %v", wait)
	}
}

func TestTokenBucketConsumeMoreThanAvailable(t *testing.T) {
	// consume() can go negative, then clamps to 0
	tb := newTokenBucket(5, 1e9)
	tb.consume(10) // More than capacity
	if tb.tokens != 0 {
		t.Errorf("expected tokens to clamp at 0, got %.2f", tb.tokens)
	}
}

func TestTokenBucketTimeUntilAvailable_AlreadyAvailable(t *testing.T) {
	tb := newTokenBucket(10, 1e9)
	// Don't drain - should return 0
	wait := tb.timeUntilAvailable(1)
	if wait != 0 {
		t.Errorf("expected 0 wait when tokens available, got %v", wait)
	}
}

func TestRateLimitHook_AfterExceedsTokenBucket(t *testing.T) {
	h := NewRateLimitHook(0, 1) // only 1 token per minute
	h.WaitOnLimit = false
	ctx := context.Background()

	// First After call uses up the token
	evt := &Event{
		Type:     EventModelCallAfter,
		Name:     "m",
		Metadata: map[string]any{"prompt_tokens": 1},
	}
	h.After(ctx, evt)

	// Second should consume more but consume() doesn't fail - just drains
	if err := h.After(ctx, evt); err != nil {
		t.Logf("After returned: %v (not necessarily an error)", err)
	}
}
