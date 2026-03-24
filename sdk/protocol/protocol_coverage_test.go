package protocol

import (
	"context"
	"testing"
	"time"
)

func TestSendAndWait_Timeout(t *testing.T) {
	b := NewBus()
	_ = b.Register("alice", "Alice", "", nil, nil)
	// Bob has no handler: message sits in bob's inbox; nobody replies to alice.
	_ = b.Register("bob", "Bob", "", nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := b.SendAndWait(ctx, &Envelope{
		Type: TypeQuestion,
		From: "alice",
		To:   "bob",
		Body: []byte(`{"question":"q"}`),
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
}

func TestSendAndWait_ContextCancelled(t *testing.T) {
	b := NewBus()
	_ = b.Register("alice", "Alice", "", nil, nil)
	_ = b.Register("bob", "Bob", "", nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	_, err := b.SendAndWait(ctx, &Envelope{
		Type: TypeQuestion,
		From: "alice",
		To:   "bob",
		Body: []byte(`{"question":"q"}`),
	})
	if err == nil {
		t.Fatal("expected cancel error")
	}
	if err != context.Canceled {
		t.Fatalf("want Canceled, got %v", err)
	}
}

func TestClose_WithPendingInboxMessages(t *testing.T) {
	b := NewBus()
	_ = b.Register("sink", "Sink", "", nil, nil)

	// Deliver to inbox without handler (queued)
	_ = b.Send(context.Background(), &Envelope{
		Type: TypeTaskRequest,
		From: "a",
		To:   "sink",
		Body: []byte(`{}`),
	})

	b.Close()
	// Close must not panic with pending messages
}

func TestDeliverToLocked_Backpressure(t *testing.T) {
	b := NewBusWithConfig(BusConfig{InboxSize: 1})
	_ = b.Register("full", "Full", "", nil, nil)

	err1 := b.Send(context.Background(), &Envelope{Type: TypeBroadcast, From: "x", To: "full", Body: []byte(`{}`)})
	if err1 != nil {
		t.Fatalf("first send: %v", err1)
	}
	err2 := b.Send(context.Background(), &Envelope{Type: TypeBroadcast, From: "x", To: "full", Body: []byte(`{}`)})
	if err2 == nil {
		t.Fatal("expected inbox full error")
	}
}

