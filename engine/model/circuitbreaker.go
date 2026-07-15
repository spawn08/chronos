package model

import (
	"sync"
	"time"
)

// cbState is the state of a circuit breaker.
type cbState int

const (
	cbClosed   cbState = iota // requests flow normally
	cbOpen                    // requests are short-circuited
	cbHalfOpen                // a single trial request is allowed through
)

// circuitBreaker is a per-provider circuit breaker. After a threshold of
// consecutive failures it opens and short-circuits calls for a cooldown period,
// then allows a single trial request ("half-open") to probe recovery. A success
// closes the breaker; a failure re-opens it.
//
// It is safe for concurrent use.
type circuitBreaker struct {
	mu               sync.Mutex
	state            cbState
	consecutiveFails int
	threshold        int
	cooldown         time.Duration
	openedAt         time.Time
	now              func() time.Time // injectable clock for tests
}

// newCircuitBreaker creates a breaker that opens after threshold consecutive
// failures and stays open for cooldown. Non-positive values fall back to
// sensible defaults (5 failures, 30s cooldown).
func newCircuitBreaker(threshold int, cooldown time.Duration) *circuitBreaker {
	if threshold <= 0 {
		threshold = 5
	}
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	return &circuitBreaker{
		state:     cbClosed,
		threshold: threshold,
		cooldown:  cooldown,
		now:       time.Now,
	}
}

// allow reports whether a request may proceed. When the breaker is open and the
// cooldown has elapsed it transitions to half-open and permits one trial.
func (cb *circuitBreaker) allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state == cbOpen {
		if cb.now().Sub(cb.openedAt) >= cb.cooldown {
			cb.state = cbHalfOpen
			return true
		}
		return false
	}
	return true
}

// recordSuccess resets the breaker to closed.
func (cb *circuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveFails = 0
	cb.state = cbClosed
}

// recordFailure records a failure, opening the breaker once the threshold is
// reached or when a half-open trial fails.
func (cb *circuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.consecutiveFails++
	if cb.state == cbHalfOpen || cb.consecutiveFails >= cb.threshold {
		cb.state = cbOpen
		cb.openedAt = cb.now()
	}
}
