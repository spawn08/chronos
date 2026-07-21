package protocol

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestBus_ConcurrentBroadcastWithChurn hammers the bus with concurrent
// broadcasts while peers register and unregister underneath. Because Unregister
// closes a peer's inbox channel, a naive delivery path could send on a closed
// channel; this test asserts the locking keeps delivery and churn serialized so
// there is no panic and no data race. Run under -race.
func TestBus_ConcurrentBroadcastWithChurn(t *testing.T) {
	b := NewBusWithConfig(BusConfig{InboxSize: 256, HistoryCap: 4096})
	defer b.Close()

	// A stable set of receivers with handlers (handlers drain via the semaphore,
	// no inbox to overflow).
	const stable = 8
	for i := 0; i < stable; i++ {
		id := "recv-" + strconv.Itoa(i)
		if err := b.Register(id, id, "", nil, func(context.Context, *Envelope) (*Envelope, error) {
			return nil, nil
		}); err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
	}
	if err := b.Register("broadcaster", "B", "", nil, nil); err != nil {
		t.Fatalf("register broadcaster: %v", err)
	}

	ctx := context.Background()
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Broadcasters.
	for g := 0; g < 6; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = b.Send(ctx, &Envelope{
					Type:    TypeBroadcast,
					From:    "broadcaster",
					To:      "*",
					Subject: "tick",
					Body:    []byte(`{}`),
				})
			}
		}()
	}

	// Churn: register/unregister transient handler peers.
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			n := 0
			for {
				select {
				case <-stop:
					return
				default:
				}
				id := fmt.Sprintf("tmp-%d-%d", g, n)
				n++
				_ = b.Register(id, id, "", nil, func(context.Context, *Envelope) (*Envelope, error) {
					return nil, nil
				})
				b.Unregister(id)
			}
		}(g)
	}

	time.Sleep(150 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestBus_ConcurrentDirectChannelsNoRace exercises DirectChannelBetween from many
// goroutines for overlapping agent pairs; the double-checked creation must return
// a single shared channel per pair without racing. Run under -race.
func TestBus_ConcurrentDirectChannelsNoRace(t *testing.T) {
	b := NewBus()
	defer b.Close()

	const pairs = 16
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := make(map[string]*DirectChannel)

	for i := 0; i < pairs; i++ {
		for dup := 0; dup < 4; dup++ { // multiple goroutines request the same pair
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				a := fmt.Sprintf("a%d", i)
				bb := fmt.Sprintf("b%d", i)
				dc := b.DirectChannelBetween(a, bb, 8)
				if dc == nil {
					t.Errorf("nil direct channel for %s/%s", a, bb)
					return
				}
				key := directKey(a, bb)
				mu.Lock()
				if prev, ok := seen[key]; ok && prev != dc {
					t.Errorf("pair %s got two different channels", key)
				}
				seen[key] = dc
				mu.Unlock()
			}(i)
		}
	}
	wg.Wait()
}

// TestBus_CloseUnblocksConcurrentWaiters verifies Close wakes every in-flight
// SendAndWait caller (no goroutine leak / indefinite block) under concurrency.
func TestBus_CloseUnblocksConcurrentWaiters(t *testing.T) {
	b := NewBus()

	// A server that never replies, so every SendAndWait blocks until Close.
	if err := b.Register("server", "S", "", nil, func(_ context.Context, _ *Envelope) (*Envelope, error) {
		select {} // block forever; the bus's Close path is what must unblock waiters
	}); err != nil {
		t.Fatalf("register server: %v", err)
	}

	const waiters = 32
	for i := 0; i < waiters; i++ {
		if err := b.Register("w"+strconv.Itoa(i), "W", "", nil, nil); err != nil {
			t.Fatalf("register waiter: %v", err)
		}
	}

	var (
		wg      sync.WaitGroup
		unblkd  atomic.Int64
		started sync.WaitGroup
	)
	started.Add(waiters)
	for i := 0; i < waiters; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			started.Done()
			_, err := b.SendAndWait(context.Background(), &Envelope{
				Type: TypeQuestion,
				From: "w" + strconv.Itoa(i),
				To:   "server",
				Body: []byte(`{}`),
			})
			if err != nil {
				unblkd.Add(1)
			}
		}(i)
	}

	started.Wait()
	time.Sleep(20 * time.Millisecond) // let waiters reach the blocking select
	b.Close()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not unblock all SendAndWait waiters")
	}
	if unblkd.Load() != waiters {
		t.Fatalf("unblocked waiters = %d, want %d", unblkd.Load(), waiters)
	}
}
