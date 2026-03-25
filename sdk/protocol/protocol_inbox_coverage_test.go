package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSendAndWait_SenderNotRegistered_Table(t *testing.T) {
	tests := []struct {
		name string
		from string
	}{
		{"missing_sender", "not-registered"},
		{"empty_sender", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBus()
			_ = b.Register("bob", "Bob", "", nil, nil)

			_, err := b.SendAndWait(context.Background(), &Envelope{
				Type: TypeQuestion,
				From: tt.from,
				To:   "bob",
				Body: []byte(`{"question":"q"}`),
			})
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "not registered") {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
}

func TestSendAndWait_BusClosedBeforeSend_Table(t *testing.T) {
	b := NewBus()
	_ = b.Register("alice", "A", "", nil, nil)
	_ = b.Register("bob", "B", "", nil, nil)
	b.Close()

	_, err := b.SendAndWait(context.Background(), &Envelope{
		Type: TypeQuestion,
		From: "alice",
		To:   "bob",
		Body: []byte(`{"question":"q"}`),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestSendAndWait_InboxClosedWhileWaiting(t *testing.T) {
	b := NewBus()
	_ = b.Register("alice", "A", "", nil, nil)
	_ = b.Register("bob", "B", "", nil, nil)

	go func() {
		time.Sleep(15 * time.Millisecond)
		b.Close()
	}()

	_, err := b.SendAndWait(context.Background(), &Envelope{
		Type: TypeTaskRequest,
		From: "alice",
		To:   "bob",
		Body: []byte(`{}`),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "closed") && !strings.Contains(err.Error(), "inbox") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestDeliverToLocked_RecipientNotFound(t *testing.T) {
	b := NewBus()
	err := b.Send(context.Background(), &Envelope{
		Type: TypeBroadcast,
		From: "a",
		To:   "nobody",
		Body: []byte(`{}`),
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestDeliverToLocked_NoInboxForPeer_Table(t *testing.T) {
	b := NewBus()
	// Internal consistency: peer exists but inbox missing — simulate by manual partial state is not possible
	// without unsafe access. Instead cover the handler error path that still delivers TypeError to sender.
	_ = b.Register("alice", "A", "", nil, nil)
	_ = b.Register("bob", "B", "", nil, func(context.Context, *Envelope) (*Envelope, error) {
		return nil, errors.New("handler boom")
	})

	reply, err := b.SendAndWait(context.Background(), &Envelope{
		Type: TypeQuestion,
		From: "alice",
		To:   "bob",
		Body: []byte(`{"question":"q"}`),
	})
	if err != nil {
		t.Fatalf("SendAndWait: %v", err)
	}
	if reply.Type != TypeError {
		t.Fatalf("want TypeError, got %v", reply.Type)
	}
	var body map[string]string
	_ = json.Unmarshal(reply.Body, &body)
	if !strings.Contains(body["error"], "handler boom") {
		t.Fatalf("unexpected error body: %v", body)
	}
}
