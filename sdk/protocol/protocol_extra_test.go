package protocol

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestDirectChannel_Close(t *testing.T) {
	dc := NewDirectChannel(4)
	// Should not panic
	dc.Close()
}

func TestSendAndWait_Success(t *testing.T) {
	b := NewBus()
	b.Register("alice", "Alice", "", nil, nil)
	b.Register("bob", "Bob", "", nil, func(ctx context.Context, env *Envelope) (*Envelope, error) {
		reply := &Envelope{
			Type:    TypeAnswer,
			From:    "bob",
			To:      "alice",
			ReplyTo: env.ID,
			Body:    []byte(`"ok"`),
		}
		return reply, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	env := &Envelope{
		Type: TypeQuestion,
		From: "alice",
		To:   "bob",
		Body: []byte(`"hello"`),
	}

	reply, err := b.SendAndWait(ctx, env)
	if err != nil {
		t.Fatalf("SendAndWait: %v", err)
	}
	if reply == nil {
		t.Fatal("expected non-nil reply")
	}
}

func TestSendAndWait_ContextCanceled(t *testing.T) {
	b := NewBus()
	b.Register("alice", "Alice", "", nil, nil)
	b.Register("bob", "Bob", "", nil, nil) // no handler, won't reply

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	env := &Envelope{
		Type: TypeQuestion,
		From: "alice",
		To:   "bob",
		Body: []byte(`"hello"`),
	}

	_, err := b.SendAndWait(ctx, env)
	if err == nil {
		t.Fatal("expected error due to context timeout")
	}
}

func TestDelegateTask_Success(t *testing.T) {
	b := NewBus()
	b.Register("manager", "Manager", "", nil, nil)
	b.Register("worker", "Worker", "", nil, func(ctx context.Context, env *Envelope) (*Envelope, error) {
		result := ResultPayload{
			TaskID:  env.ID,
			Success: true,
			Summary: "task done",
		}
		body, _ := json.Marshal(result)
		reply := &Envelope{
			Type:    TypeTaskResult,
			From:    "worker",
			To:      "manager",
			ReplyTo: env.ID,
			Body:    body,
		}
		return reply, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := b.DelegateTask(ctx, "manager", "worker", "do-task", TaskPayload{
		Description: "process something",
		Input:       map[string]any{"data": "hello"},
	})
	if err != nil {
		t.Fatalf("DelegateTask: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestAsk_Success(t *testing.T) {
	b := NewBus()
	b.Register("asker", "Asker", "", nil, nil)
	b.Register("answerer", "Answerer", "", nil, func(ctx context.Context, env *Envelope) (*Envelope, error) {
		answer := map[string]string{"answer": "42"}
		body, _ := json.Marshal(answer)
		reply := &Envelope{
			Type:    TypeAnswer,
			From:    "answerer",
			To:      "asker",
			ReplyTo: env.ID,
			Body:    body,
		}
		return reply, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	answer, err := b.Ask(ctx, "asker", "answerer", "what is the answer?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if answer != "42" {
		t.Errorf("expected '42', got %q", answer)
	}
}
