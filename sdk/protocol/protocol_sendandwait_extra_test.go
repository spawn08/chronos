package protocol

import (
	"context"
	"testing"
	"time"
)

func TestSendAndWait_SenderNotRegistered(t *testing.T) {
	b := NewBus()
	b.Register("bob", "Bob", "", nil, func(ctx context.Context, env *Envelope) (*Envelope, error) {
		return &Envelope{
			Type:    TypeAnswer,
			From:    "bob",
			To:      "alice",
			ReplyTo: env.ID,
			Body:    []byte(`"ok"`),
		}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := b.SendAndWait(ctx, &Envelope{
		Type: TypeQuestion,
		From: "alice",
		To:   "bob",
		Body: []byte(`"hi"`),
	})
	if err == nil {
		t.Fatal("expected error: sender not registered")
	}
}

func TestSendAndWait_SendFailsWhenBusClosed(t *testing.T) {
	b := NewBus()
	b.Register("alice", "Alice", "", nil, nil)
	b.Register("bob", "Bob", "", nil, func(ctx context.Context, env *Envelope) (*Envelope, error) {
		return &Envelope{Type: TypeAnswer, From: "bob", To: "alice", ReplyTo: env.ID, Body: []byte(`"ok"`)}, nil
	})
	b.Close()

	ctx := context.Background()
	_, err := b.SendAndWait(ctx, &Envelope{
		Type: TypeQuestion,
		From: "alice",
		To:   "bob",
		Body: []byte(`"q"`),
	})
	if err == nil {
		t.Fatal("expected error when bus is closed")
	}
}
