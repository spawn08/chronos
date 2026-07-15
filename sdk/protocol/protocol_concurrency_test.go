package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSendAndWait_ConcurrentNoMisdelivery stresses the correlation-map reply
// routing: many senders each issue a SendAndWait against a shared worker inbox
// and every caller must receive exactly the reply that corresponds to its own
// request. Run with -race to catch data races in the routing path.
func TestSendAndWait_ConcurrentNoMisdelivery(t *testing.T) {
	const (
		numSenders     = 64
		perSenderCalls = 25
	)

	b := NewBusWithConfig(BusConfig{
		InboxSize:             2048,
		MaxConcurrentHandlers: numSenders * perSenderCalls, // never saturate here
	})
	defer b.Close()

	// The worker echoes the request's correlation id back inside the reply body
	// so each caller can assert it received the reply for its own request.
	if err := b.Register("worker", "Worker", "", nil, func(_ context.Context, env *Envelope) (*Envelope, error) {
		body, _ := json.Marshal(map[string]string{"echo": env.ID})
		return &Envelope{Type: TypeAnswer, Body: body}, nil
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	for s := 0; s < numSenders; s++ {
		if err := b.Register(fmt.Sprintf("sender-%d", s), "S", "", nil, nil); err != nil {
			t.Fatalf("register sender: %v", err)
		}
	}

	var (
		wg        sync.WaitGroup
		mismatch  atomic.Int64
		errCount  atomic.Int64
		succeeded atomic.Int64
	)

	for s := 0; s < numSenders; s++ {
		wg.Add(1)
		go func(sender int) {
			defer wg.Done()
			from := fmt.Sprintf("sender-%d", sender)
			for c := 0; c < perSenderCalls; c++ {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				body, _ := json.Marshal(map[string]int{"sender": sender, "call": c})
				reply, err := b.SendAndWait(ctx, &Envelope{
					Type:    TypeQuestion,
					From:    from,
					To:      "worker",
					Subject: "ping",
					Body:    body,
				})
				cancel()
				if err != nil {
					errCount.Add(1)
					continue
				}
				var got map[string]string
				if uerr := json.Unmarshal(reply.Body, &got); uerr != nil {
					mismatch.Add(1)
					continue
				}
				// The reply's ReplyTo must equal the request id, and the echoed
				// id inside the body must match too — a mis-delivered reply
				// would carry a different correlation id.
				if reply.ReplyTo == "" || got["echo"] != reply.ReplyTo {
					mismatch.Add(1)
					continue
				}
				succeeded.Add(1)
			}
		}(s)
	}
	wg.Wait()

	if mismatch.Load() != 0 {
		t.Fatalf("reply mis-delivery detected: %d mismatched replies", mismatch.Load())
	}
	if errCount.Load() != 0 {
		t.Fatalf("unexpected SendAndWait errors: %d", errCount.Load())
	}
	if want := int64(numSenders * perSenderCalls); succeeded.Load() != want {
		t.Fatalf("expected %d successful replies, got %d", want, succeeded.Load())
	}
}

// TestHandlerPool_BoundsConcurrency verifies the handler-goroutine cap is
// observably enforced: with a small MaxConcurrentHandlers and deliberately slow
// handlers, the number of handlers running at once never exceeds the cap, and a
// flood of deliveries produces back-pressure errors rather than unbounded
// goroutine growth.
func TestHandlerPool_BoundsConcurrency(t *testing.T) {
	const (
		maxHandlers = 4
		numSenders  = 60
	)

	b := NewBusWithConfig(BusConfig{
		InboxSize:             1024,
		MaxConcurrentHandlers: maxHandlers,
	})
	defer b.Close()

	release := make(chan struct{})
	var (
		active atomic.Int64
		peak   atomic.Int64
	)

	if err := b.Register("worker", "Worker", "", nil, func(_ context.Context, _ *Envelope) (*Envelope, error) {
		cur := active.Add(1)
		for {
			p := peak.Load()
			if cur <= p || peak.CompareAndSwap(p, cur) {
				break
			}
		}
		<-release // hold the slot until the test lets go
		active.Add(-1)
		return &Envelope{Type: TypeAck}, nil
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}
	if err := b.Register("sender", "Sender", "", nil, nil); err != nil {
		t.Fatalf("register sender: %v", err)
	}

	var (
		wg           sync.WaitGroup
		accepted     atomic.Int64
		backpressure atomic.Int64
	)
	for i := 0; i < numSenders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := b.Send(context.Background(), &Envelope{
				Type:    TypeTaskRequest,
				From:    "sender",
				To:      "worker",
				Subject: "work",
				Body:    json.RawMessage(fmt.Sprintf(`{"n":%d}`, i)),
			})
			switch {
			case err == nil:
				accepted.Add(1)
			case strings.Contains(err.Error(), "back-pressure"):
				backpressure.Add(1)
			default:
				t.Errorf("unexpected send error: %v", err)
			}
		}(i)
	}
	wg.Wait()

	// Wait until every accepted handler has actually started running (and is
	// now blocked on release) so the peak counter is fully settled.
	deadline := time.After(2 * time.Second)
	for active.Load() < accepted.Load() {
		select {
		case <-deadline:
			t.Fatalf("handlers did not all start; active=%d accepted=%d", active.Load(), accepted.Load())
		default:
			time.Sleep(time.Millisecond)
		}
	}

	if peak.Load() > int64(maxHandlers) {
		t.Fatalf("concurrent handlers exceeded cap: peak=%d cap=%d", peak.Load(), maxHandlers)
	}
	if accepted.Load() > int64(maxHandlers) {
		t.Fatalf("more handlers accepted than cap while all block: accepted=%d cap=%d", accepted.Load(), maxHandlers)
	}
	if backpressure.Load() == 0 {
		t.Fatalf("expected back-pressure errors under a flood, got none (accepted=%d)", accepted.Load())
	}

	close(release) // let the running handlers finish
}

// TestHandlerPool_SlotsReleased confirms that handler slots are returned to the
// pool after handlers finish, so throughput is not permanently throttled.
func TestHandlerPool_SlotsReleased(t *testing.T) {
	b := NewBusWithConfig(BusConfig{MaxConcurrentHandlers: 2})
	defer b.Close()

	if err := b.Register("alice", "A", "", nil, nil); err != nil {
		t.Fatalf("register alice: %v", err)
	}
	if err := b.Register("bob", "B", "", nil, func(_ context.Context, env *Envelope) (*Envelope, error) {
		return &Envelope{Type: TypeAnswer, Body: env.Body}, nil
	}); err != nil {
		t.Fatalf("register bob: %v", err)
	}

	// Far more sequential calls than the cap: each must succeed because the
	// previous handler releases its slot before the next call runs.
	for i := 0; i < 50; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := b.SendAndWait(ctx, &Envelope{
			Type: TypeQuestion,
			From: "alice",
			To:   "bob",
			Body: json.RawMessage(fmt.Sprintf(`{"i":%d}`, i)),
		})
		cancel()
		if err != nil {
			t.Fatalf("call %d failed (slot not released?): %v", i, err)
		}
	}
}

// TestHandler_ReceivesCallerContext verifies the caller's context reaches the
// handler and that handler-side cancellation is observable.
func TestHandler_ReceivesCallerContext(t *testing.T) {
	t.Run("deadline propagated", func(t *testing.T) {
		b := NewBus()
		defer b.Close()

		gotDeadline := make(chan bool, 1)
		_ = b.Register("alice", "A", "", nil, nil)
		_ = b.Register("bob", "B", "", nil, func(ctx context.Context, env *Envelope) (*Envelope, error) {
			_, ok := ctx.Deadline()
			gotDeadline <- ok
			return &Envelope{Type: TypeAnswer, Body: env.Body}, nil
		})

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := b.SendAndWait(ctx, &Envelope{Type: TypeQuestion, From: "alice", To: "bob", Body: []byte(`{}`)}); err != nil {
			t.Fatalf("SendAndWait: %v", err)
		}
		select {
		case ok := <-gotDeadline:
			if !ok {
				t.Fatal("handler did not observe the caller's deadline")
			}
		case <-time.After(time.Second):
			t.Fatal("handler was not invoked")
		}
	})

	t.Run("cancellation observed by handler", func(t *testing.T) {
		b := NewBus()
		defer b.Close()

		canceled := make(chan struct{}, 1)
		started := make(chan struct{}, 1)
		_ = b.Register("alice", "A", "", nil, nil)
		_ = b.Register("bob", "B", "", nil, func(ctx context.Context, _ *Envelope) (*Envelope, error) {
			started <- struct{}{}
			<-ctx.Done()
			canceled <- struct{}{}
			return nil, ctx.Err()
		})

		ctx, cancel := context.WithCancel(context.Background())
		// Fire-and-forget so we control the context lifetime independently.
		if err := b.Send(ctx, &Envelope{Type: TypeTaskRequest, From: "alice", To: "bob", Body: []byte(`{}`)}); err != nil {
			t.Fatalf("Send: %v", err)
		}
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("handler was not invoked")
		}
		cancel()
		select {
		case <-canceled:
		case <-time.After(time.Second):
			t.Fatal("handler did not observe context cancellation")
		}
	})
}
