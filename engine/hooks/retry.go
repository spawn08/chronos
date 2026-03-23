package hooks

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/spawn08/chronos/engine/model"
)

// RetryHook retries failed model calls with exponential backoff and jitter.
// Unlike a metadata-only hook, this hook performs actual retries by re-invoking
// the model provider when a model call fails.
//
// Usage:
//
//	hook := hooks.NewRetryHook(3)
//	agent.New("id", "name").AddHook(hook).Build()
type RetryHook struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration

	// RetryableError optionally classifies errors. If nil, all errors trigger retry.
	RetryableError func(err error) bool

	// OnRetry is called when a retry is about to be attempted. It receives the
	// attempt number (1-based) and the delay that will be applied.
	OnRetry func(attempt int, delay time.Duration)

	// Retries tracks the total number of retries performed (for observability).
	Retries int

	// SleepFn is the function used to sleep between retries.
	// Defaults to time.Sleep. Override in tests for instant execution.
	SleepFn func(time.Duration)
}

// NewRetryHook creates a retry hook with sensible defaults.
func NewRetryHook(maxRetries int) *RetryHook {
	if maxRetries <= 0 {
		maxRetries = 3
	}
	return &RetryHook{
		MaxRetries: maxRetries,
		BaseDelay:  500 * time.Millisecond,
		MaxDelay:   30 * time.Second,
		SleepFn:    time.Sleep,
	}
}

func (h *RetryHook) Before(_ context.Context, _ *Event) error {
	return nil
}

// After inspects model call errors and performs actual retries.
// When a model call fails with a retryable error, this hook:
//  1. Waits with exponential backoff + jitter
//  2. Re-invokes the model provider using the original request
//  3. Updates the event output/error with the retry result
//  4. Repeats up to MaxRetries times
func (h *RetryHook) After(ctx context.Context, evt *Event) error {
	if evt.Type != EventModelCallAfter {
		return nil
	}
	if evt.Error == nil {
		return nil
	}
	if h.RetryableError != nil && !h.RetryableError(evt.Error) {
		return nil
	}

	provider, _ := evt.Metadata["provider"].(model.Provider)
	req, _ := evt.Metadata["request"].(*model.ChatRequest)
	if provider == nil || req == nil {
		// Fallback: signal retry via metadata for callers that don't pass provider/request.
		// This preserves backward compatibility with the old metadata-only behavior.
		h.signalRetry(evt)
		return nil
	}

	sleepFn := h.SleepFn
	if sleepFn == nil {
		sleepFn = time.Sleep
	}

	var lastErr error
	for attempt := 1; attempt <= h.MaxRetries; attempt++ {
		delay := h.backoff(attempt)
		if h.OnRetry != nil {
			h.OnRetry(attempt, delay)
		}
		h.Retries++

		sleepFn(delay)

		if ctx.Err() != nil {
			return fmt.Errorf("retry canceled: %w", ctx.Err())
		}

		resp, err := provider.Chat(ctx, req)
		if err == nil {
			evt.Output = resp
			evt.Error = nil
			if evt.Metadata == nil {
				evt.Metadata = make(map[string]any)
			}
			evt.Metadata["retry_attempts"] = attempt
			evt.Metadata["retry_success"] = true
			return nil
		}

		lastErr = err
		if h.RetryableError != nil && !h.RetryableError(err) {
			break
		}
	}

	evt.Error = lastErr
	if evt.Metadata == nil {
		evt.Metadata = make(map[string]any)
	}
	evt.Metadata["retry_attempts"] = h.MaxRetries
	evt.Metadata["retry_success"] = false
	return nil
}

// signalRetry sets metadata on the event to signal the caller that a retry
// should be attempted. Used when provider/request are not available in metadata.
func (h *RetryHook) signalRetry(evt *Event) {
	attempt := 1
	if v, ok := evt.Metadata["retry_attempt"].(int); ok {
		attempt = v
	}
	if attempt > h.MaxRetries {
		return
	}

	delay := h.backoff(attempt)
	if h.OnRetry != nil {
		h.OnRetry(attempt, delay)
	}
	h.Retries++

	if evt.Metadata == nil {
		evt.Metadata = make(map[string]any)
	}
	evt.Metadata["retry"] = true
	evt.Metadata["retry_attempt"] = attempt + 1
	evt.Metadata["retry_delay"] = delay
}

func (h *RetryHook) backoff(attempt int) time.Duration {
	base := float64(h.BaseDelay)
	if base <= 0 {
		base = float64(500 * time.Millisecond)
	}
	maxD := float64(h.MaxDelay)
	if maxD <= 0 {
		maxD = float64(30 * time.Second)
	}
	delay := base * math.Pow(2, float64(attempt-1))
	// Add jitter: ±25%
	jitter := delay * 0.25 * (rand.Float64()*2 - 1)
	delay += jitter
	if delay > maxD {
		delay = maxD
	}
	return time.Duration(delay)
}
