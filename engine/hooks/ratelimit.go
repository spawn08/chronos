package hooks

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RateLimitHook enforces per-provider rate limits using a token-bucket algorithm.
// It blocks (or returns an error) on EventModelCallBefore if the rate limit
// would be exceeded.
type RateLimitHook struct {
	mu sync.Mutex

	// RequestsPerMinute caps the number of model calls per minute. 0 = unlimited.
	RequestsPerMinute int
	// TokensPerMinute caps the estimated prompt tokens per minute. 0 = unlimited.
	TokensPerMinute int
	// WaitOnLimit controls whether the hook blocks until capacity is available
	// (true) or returns an error immediately (false). Default: true.
	WaitOnLimit bool

	requestBucket *tokenBucket
	tokenBucket_  *tokenBucket
}

// NewRateLimitHook creates a rate limit hook.
func NewRateLimitHook(requestsPerMinute, tokensPerMinute int) *RateLimitHook {
	h := &RateLimitHook{
		RequestsPerMinute: requestsPerMinute,
		TokensPerMinute:   tokensPerMinute,
		WaitOnLimit:       true,
	}
	if requestsPerMinute > 0 {
		h.requestBucket = newTokenBucket(requestsPerMinute, time.Minute)
	}
	if tokensPerMinute > 0 {
		h.tokenBucket_ = newTokenBucket(tokensPerMinute, time.Minute)
	}
	return h
}

func (h *RateLimitHook) Before(ctx context.Context, evt *Event) error {
	if evt.Type != EventModelCallBefore {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.requestBucket != nil {
		if err := h.waitOrFail(ctx, h.requestBucket, 1); err != nil {
			return fmt.Errorf("rate limit (requests): %w", err)
		}
	}
	return nil
}

func (h *RateLimitHook) After(_ context.Context, evt *Event) error {
	if evt.Type != EventModelCallAfter {
		return nil
	}
	// Deduct actual tokens from the token bucket based on usage metadata.
	if h.tokenBucket_ == nil {
		return nil
	}
	if evt.Metadata == nil {
		return nil
	}
	tokens, _ := evt.Metadata["prompt_tokens"].(int)
	if tokens > 0 {
		h.mu.Lock()
		h.tokenBucket_.consume(tokens)
		h.mu.Unlock()
	}
	return nil
}

func (h *RateLimitHook) waitOrFail(ctx context.Context, tb *tokenBucket, n int) error {
	if tb.tryConsume(n) {
		return nil
	}
	if !h.WaitOnLimit {
		return fmt.Errorf("limit exceeded, try again later")
	}
	// Wait until tokens are available or context is cancelled
	for {
		wait := tb.timeUntilAvailable(n)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
			if tb.tryConsume(n) {
				return nil
			}
		}
	}
}

// tokenBucket is a simple token-bucket rate limiter.
type tokenBucket struct {
	capacity   int
	tokens     float64
	refillRate float64 // tokens per nanosecond
	lastRefill time.Time
}

func newTokenBucket(capacity int, window time.Duration) *tokenBucket {
	return &tokenBucket{
		capacity:   capacity,
		tokens:     float64(capacity),
		refillRate: float64(capacity) / float64(window),
		lastRefill: time.Now(),
	}
}

func (tb *tokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill)
	tb.tokens += float64(elapsed) * tb.refillRate
	if tb.tokens > float64(tb.capacity) {
		tb.tokens = float64(tb.capacity)
	}
	tb.lastRefill = now
}

func (tb *tokenBucket) tryConsume(n int) bool {
	tb.refill()
	if tb.tokens >= float64(n) {
		tb.tokens -= float64(n)
		return true
	}
	return false
}

func (tb *tokenBucket) consume(n int) {
	tb.refill()
	tb.tokens -= float64(n)
	if tb.tokens < 0 {
		tb.tokens = 0
	}
}

func (tb *tokenBucket) timeUntilAvailable(n int) time.Duration {
	tb.refill()
	deficit := float64(n) - tb.tokens
	if deficit <= 0 {
		return 0
	}
	return time.Duration(deficit / tb.refillRate)
}
