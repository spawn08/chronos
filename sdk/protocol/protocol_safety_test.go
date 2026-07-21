package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// TestHandlerPanicRecovery verifies that a peer handler which panics is
// recovered: the process survives and the sender receives an error reply.
func TestHandlerPanicRecovery(t *testing.T) {
	tests := []struct {
		name    string
		handler Handler
	}{
		{
			name: "string panic",
			handler: func(_ context.Context, _ *Envelope) (*Envelope, error) {
				panic("handler boom")
			},
		},
		{
			name: "error panic",
			handler: func(_ context.Context, _ *Envelope) (*Envelope, error) {
				panic(errors.New("handler kaboom"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBus()
			defer b.Close()

			// Sender has no handler so replies land in its inbox.
			if err := b.Register("sender", "Sender", "", nil, nil); err != nil {
				t.Fatalf("register sender: %v", err)
			}
			if err := b.Register("worker", "Worker", "", nil, tt.handler); err != nil {
				t.Fatalf("register worker: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			body, _ := json.Marshal(map[string]string{"q": "hi"})
			reply, err := b.SendAndWait(ctx, &Envelope{
				Type:    TypeTaskRequest,
				From:    "sender",
				To:      "worker",
				Subject: "do work",
				Body:    body,
			})
			if err != nil {
				t.Fatalf("SendAndWait returned error (process should survive panic): %v", err)
			}
			if reply == nil {
				t.Fatal("expected an error reply, got nil")
				return
			}
			if reply.Type != TypeError {
				t.Fatalf("expected TypeError reply, got %q", reply.Type)
			}

			// Bus still works: a healthy handler responds normally.
			if regErr := b.Register("healthy", "Healthy", "", nil, func(_ context.Context, env *Envelope) (*Envelope, error) {
				return &Envelope{Type: TypeAck, Body: env.Body}, nil
			}); regErr != nil {
				t.Fatalf("register healthy: %v", regErr)
			}
			reply2, err := b.SendAndWait(ctx, &Envelope{
				Type:    TypeTaskRequest,
				From:    "sender",
				To:      "healthy",
				Subject: "ping",
				Body:    body,
			})
			if err != nil {
				t.Fatalf("healthy SendAndWait failed after panic: %v", err)
			}
			if reply2.Type != TypeAck {
				t.Fatalf("expected TypeAck, got %q", reply2.Type)
			}
		})
	}
}

// TestInvokeHandlerRecovers unit-tests the recovery helper directly.
func TestInvokeHandlerRecovers(t *testing.T) {
	reply, err := invokeHandler(context.Background(), func(_ context.Context, _ *Envelope) (*Envelope, error) {
		panic("nope")
	}, &Envelope{})
	if err == nil {
		t.Fatal("expected error from panicking handler")
		return
	}
	if reply != nil {
		t.Fatalf("expected nil reply, got %v", reply)
	}
}
