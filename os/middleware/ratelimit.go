package middleware

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// RateLimitConfig holds configuration for the rate limiter.
type RateLimitConfig struct {
	RequestsPerWindow int
	Window            time.Duration
	KeyFunc           func(r *http.Request) string
}

// DefaultRateLimitConfig returns a rate limiter that allows 100 requests
// per minute keyed by client IP.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerWindow: 100,
		Window:            time.Minute,
		KeyFunc:           IPKeyFunc,
	}
}

// IPKeyFunc extracts the client IP for rate limiting.
func IPKeyFunc(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

type bucket struct {
	count   int
	resetAt time.Time
}

// RateLimit returns middleware that limits requests using a fixed-window counter.
func RateLimit(cfg RateLimitConfig) func(http.Handler) http.Handler {
	var mu sync.Mutex
	buckets := make(map[string]*bucket)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := cfg.KeyFunc(r)
			now := time.Now()

			mu.Lock()
			b, ok := buckets[key]
			if !ok || now.After(b.resetAt) {
				b = &bucket{count: 0, resetAt: now.Add(cfg.Window)}
				buckets[key] = b
			}
			b.count++
			count := b.count
			resetAt := b.resetAt
			remaining := cfg.RequestsPerWindow - count
			mu.Unlock()

			w.Header().Set("X-RateLimit-Limit", itoa(cfg.RequestsPerWindow))
			w.Header().Set("X-RateLimit-Remaining", itoa(max(0, remaining)))
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetAt.Unix()))

			if count > cfg.RequestsPerWindow {
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(time.Until(resetAt).Seconds())+1))
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
