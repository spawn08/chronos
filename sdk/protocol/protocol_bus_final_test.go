package protocol

import (
	"context"
	"testing"
	"time"
)

func TestSendAndWait_RequeuesNonMatchingReply(t *testing.T) {
	b := NewBus()
	if err := b.Register("alice", "Alice", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := b.Register("bob", "Bob", "", nil, func(_ context.Context, env *Envelope) (*Envelope, error) {
		return &Envelope{
			Type: TypeAnswer,
			Body: []byte(`"ok"`),
		}, nil
	}); err != nil {
		t.Fatal(err)
	}

	b.mu.Lock()
	ch := b.inbox["alice"]
	b.mu.Unlock()

	ch <- &Envelope{
		ReplyTo: "noise-id",
		From:    "ghost",
		To:      "alice",
		Body:    []byte(`"ignore"`),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	env := &Envelope{
		ID:      "req-1",
		Type:    TypeQuestion,
		From:    "alice",
		To:      "bob",
		Subject: "q",
		Body:    []byte(`"hello"`),
	}

	reply, err := b.SendAndWait(ctx, env)
	if err != nil {
		t.Fatal(err)
	}
	if reply.ReplyTo != "req-1" {
		t.Fatalf("ReplyTo = %q", reply.ReplyTo)
	}
}

func TestSendAndWait_ContextCanceledWaitingForReply(t *testing.T) {
	b := NewBus()
	_ = b.Register("alice", "Alice", "", nil, nil)
	_ = b.Register("bob", "Bob", "", nil, func(_ context.Context, env *Envelope) (*Envelope, error) {
		time.Sleep(500 * time.Millisecond)
		return &Envelope{Type: TypeAnswer, Body: []byte(`"late"`)}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := b.SendAndWait(ctx, &Envelope{
		ID:   "r2",
		Type: TypeQuestion,
		From: "alice",
		To:   "bob",
		Body: []byte(`"q"`),
	})
	if err != context.Canceled {
		t.Fatalf("want canceled, got %v", err)
	}
}
