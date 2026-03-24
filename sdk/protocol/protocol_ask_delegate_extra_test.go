package protocol

import (
	"context"
	"testing"
	"time"
)

func TestAsk_NonJSONAnswerBody(t *testing.T) {
	b := NewBus()
	b.Register("alice", "Alice", "", nil, nil)
	b.Register("bob", "Bob", "", nil, func(ctx context.Context, env *Envelope) (*Envelope, error) {
		return &Envelope{
			Type:    TypeAnswer,
			From:    "bob",
			To:      "alice",
			ReplyTo: env.ID,
			Body:    []byte(`not JSON but plain text`),
		}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := b.Ask(ctx, "alice", "bob", "why?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if out != "not JSON but plain text" {
		t.Errorf("got %q", out)
	}
}

func TestDelegateTask_InvalidResultJSON(t *testing.T) {
	b := NewBus()
	b.Register("mgr", "Manager", "", nil, nil)
	b.Register("worker", "Worker", "", nil, func(ctx context.Context, env *Envelope) (*Envelope, error) {
		return &Envelope{
			Type:    TypeTaskResult,
			From:    "worker",
			To:      "mgr",
			ReplyTo: env.ID,
			Body:    []byte(`not-json`),
		}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := b.DelegateTask(ctx, "mgr", "worker", "job", TaskPayload{Description: "x"})
	if err == nil {
		t.Fatal("expected decode error")
	}
}
