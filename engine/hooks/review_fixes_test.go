package hooks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/model"
)

// TestRetryHook_DefaultSleepAbortsOnContextCancel verifies the default (no
// SleepFn) backoff is context-aware: canceling the context mid-wait aborts the
// retry promptly instead of blocking for the full BaseDelay/MaxDelay. Regression
// for the review finding that time.Sleep ignored ctx (up to 30s unresponsive).
func TestRetryHook_DefaultSleepAbortsOnContextCancel(t *testing.T) {
	h := NewRetryHook(3)
	h.BaseDelay = 10 * time.Second // would block for seconds if ctx were ignored
	h.MaxDelay = 30 * time.Second
	// Deliberately leave SleepFn nil to exercise the default ctx-aware path.

	provider := &mockProvider{errors: []error{errors.New("boom"), errors.New("boom"), errors.New("boom")}}
	evt := &Event{
		Type:  EventModelCallAfter,
		Error: errors.New("boom"),
		Metadata: map[string]any{
			"provider": model.Provider(provider),
			"request":  &model.ChatRequest{},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := h.After(ctx, evt)
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Fatalf("After blocked ignoring ctx cancellation: took %v", elapsed)
	}
	if err == nil {
		t.Fatal("expected a cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// TestRateLimitHook_TokensEnforcedInBefore verifies TokensPerMinute applies real
// back-pressure in Before (not just accounting in After). Regression for the
// finding that TokensPerMinute was never enforced.
func TestRateLimitHook_TokensEnforcedInBefore(t *testing.T) {
	h := NewRateLimitHook(0, 5) // no request cap; 5 tokens/min
	h.WaitOnLimit = false       // fail fast instead of blocking

	// A request whose estimated prompt tokens exceed the tiny bucket must be
	// rejected in Before.
	big := &model.ChatRequest{Messages: []model.Message{
		{Role: model.RoleUser, Content: "this is a reasonably long prompt that should estimate to well over five tokens"},
	}}
	evt := &Event{Type: EventModelCallBefore, Input: big, Metadata: map[string]any{"request": big}}

	if err := h.Before(context.Background(), evt); err == nil {
		t.Fatal("expected Before to reject when estimated tokens exceed the per-minute cap")
	}
}

// TestRateLimitHook_ReconcilesReservation verifies After reconciles the estimate
// reserved in Before against actual usage without double-counting: an
// over-estimate is refunded, an under-estimate is topped up.
func TestRateLimitHook_ReconcilesReservation(t *testing.T) {
	h := NewRateLimitHook(0, 1000)

	// Simulate Before reserving 100 tokens (as waitOrFail would).
	h.tokenBkt.consume(100)

	// Actual usage was only 40, so After refunds 60, leaving a net 40 consumed.
	evt := &Event{Type: EventModelCallAfter, Metadata: map[string]any{
		ratelimitReservedKey: 100,
		"prompt_tokens":      40,
	}}
	if err := h.After(context.Background(), evt); err != nil {
		t.Fatalf("After: %v", err)
	}
	if got := h.tokenBkt.available(); got < 959 || got > 961 {
		t.Fatalf("expected ~960 tokens available after net-40 consumption, got %d", got)
	}
}
