package model

import (
	"sync"
	"testing"
	"time"
)

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := newCircuitBreaker(3, time.Minute)
	if !cb.allow() {
		t.Fatal("breaker should start closed")
	}
	for i := 0; i < 3; i++ {
		cb.recordFailure()
	}
	if cb.allow() {
		t.Fatal("breaker should be open after threshold failures")
	}
}

func TestCircuitBreaker_SuccessResets(t *testing.T) {
	cb := newCircuitBreaker(3, time.Minute)
	cb.recordFailure()
	cb.recordFailure()
	cb.recordSuccess() // resets consecutive count
	cb.recordFailure()
	cb.recordFailure()
	if !cb.allow() {
		t.Fatal("breaker should remain closed: success reset the failure count")
	}
}

func TestCircuitBreaker_HalfOpenRecovery(t *testing.T) {
	now := time.Now()
	cb := newCircuitBreaker(2, 30*time.Second)
	cb.now = func() time.Time { return now }

	cb.recordFailure()
	cb.recordFailure()
	if cb.allow() {
		t.Fatal("expected open breaker")
	}

	// Advance past cooldown → half-open, one trial allowed.
	now = now.Add(31 * time.Second)
	if !cb.allow() {
		t.Fatal("expected half-open trial to be allowed after cooldown")
	}

	// A failing trial re-opens immediately.
	cb.recordFailure()
	if cb.allow() {
		t.Fatal("failed half-open trial should re-open the breaker")
	}

	// After cooldown again, a successful trial closes it.
	now = now.Add(31 * time.Second)
	if !cb.allow() {
		t.Fatal("expected half-open trial after second cooldown")
	}
	cb.recordSuccess()
	if !cb.allow() {
		t.Fatal("successful trial should close the breaker")
	}
}

func TestCircuitBreaker_ConcurrentRace(t *testing.T) {
	cb := newCircuitBreaker(50, time.Millisecond)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				cb.allow()
				if (n+j)%2 == 0 {
					cb.recordFailure()
				} else {
					cb.recordSuccess()
				}
			}
		}(i)
	}
	wg.Wait()
}
