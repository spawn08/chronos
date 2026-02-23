package hooks

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// RetryHook retries failed model calls with exponential backoff and jitter.
// It intercepts EventModelCallAfter; when an error is detected it replays the
// model call up to MaxRetries times. The retry is performed by re-invoking the
// provider through a caller-supplied function (set via OnRetry).
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
	}
}

func (h *RetryHook) Before(_ context.Context, _ *Event) error {
	return nil
}

// After inspects model call errors and records that a retry should happen.
// The actual retry loop must be implemented by the caller (agent) because
// the hook system cannot re-invoke the provider directly. This hook sets
// metadata on the event to signal the caller.
func (h *RetryHook) After(_ context.Context, evt *Event) error {
	if evt.Type != EventModelCallAfter {
		return nil
	}
	if evt.Error == nil {
		return nil
	}
	if h.RetryableError != nil && !h.RetryableError(evt.Error) {
		return nil
	}

	attempt := 1
	if v, ok := evt.Metadata["retry_attempt"].(int); ok {
		attempt = v
	}
	if attempt > h.MaxRetries {
		return nil
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

	return nil
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
