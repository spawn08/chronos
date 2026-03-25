package protocol

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestMessageTypes(t *testing.T) {
	types := []MessageType{
		TypeTaskRequest, TypeTaskResult, TypeQuestion, TypeAnswer,
		TypeBroadcast, TypeAck, TypeError, TypeHandoff, TypeStatus,
	}
	for _, mt := range types {
		if mt == "" {
			t.Error("message type must not be empty")
		}
	}
}

func TestPriorityConstants(t *testing.T) {
	if PriorityLow >= PriorityNormal {
		t.Error("Low must be less than Normal")
	}
	if PriorityNormal >= PriorityHigh {
		t.Error("Normal must be less than High")
	}
	if PriorityHigh >= PriorityUrgent {
		t.Error("High must be less than Urgent")
	}
}

func TestAcquireReleaseEnvelope(t *testing.T) {
	e := AcquireEnvelope()
	if e == nil {
		t.Fatal("AcquireEnvelope returned nil")
	}
	e.ID = "test-id"
	ReleaseEnvelope(e)
	// After release the envelope should be zeroed
	if e.ID != "" {
		t.Errorf("expected ID to be cleared after release, got %q", e.ID)
	}
}

func TestNewDirectChannel(t *testing.T) {
	dc := NewDirectChannel(10)
	if dc == nil {
		t.Fatal("NewDirectChannel returned nil")
	}
	if cap(dc.AtoB) != 10 {
		t.Errorf("expected AtoB cap 10, got %d", cap(dc.AtoB))
	}
	if cap(dc.BtoA) != 10 {
		t.Errorf("expected BtoA cap 10, got %d", cap(dc.BtoA))
	}
}

func TestNewDirectChannelDefaultSize(t *testing.T) {
	dc := NewDirectChannel(0)
	if cap(dc.AtoB) != 64 {
		t.Errorf("expected default cap 64, got %d", cap(dc.AtoB))
	}
}

func TestDirectKey(t *testing.T) {
	k1 := directKey("alice", "bob")
	k2 := directKey("bob", "alice")
	if k1 != k2 {
		t.Errorf("directKey not symmetric: %q vs %q", k1, k2)
	}
}

func TestNewBus(t *testing.T) {
	b := NewBus()
	if b == nil {
		t.Fatal("NewBus returned nil")
	}
	if len(b.Peers()) != 0 {
		t.Error("new bus should have no peers")
	}
}

func TestNewBusWithConfig(t *testing.T) {
	b := NewBusWithConfig(BusConfig{InboxSize: 10, HistoryCap: 20})
	if b.inboxSize != 10 {
		t.Errorf("expected inboxSize 10, got %d", b.inboxSize)
	}
	if b.histCap != 20 {
		t.Errorf("expected histCap 20, got %d", b.histCap)
	}
}

func TestRegisterAndPeers(t *testing.T) {
	b := NewBus()
	err := b.Register("a1", "Agent1", "desc", []string{"cap1"}, nil)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	peers := b.Peers()
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer, got %d", len(peers))
	}
	if peers[0].ID != "a1" {
		t.Errorf("peer ID: got %q", peers[0].ID)
	}
	if peers[0].Name != "Agent1" {
		t.Errorf("peer Name: got %q", peers[0].Name)
	}
}

func TestRegisterDuplicate(t *testing.T) {
	b := NewBus()
	b.Register("a1", "Agent1", "", nil, nil)
	err := b.Register("a1", "Agent1-dup", "", nil, nil)
	if err == nil {
		t.Error("expected error on duplicate registration")
	}
}

func TestUnregister(t *testing.T) {
	b := NewBus()
	b.Register("a1", "Agent1", "", nil, nil)
	b.Unregister("a1")
	if len(b.Peers()) != 0 {
		t.Error("expected 0 peers after unregister")
	}
}

func TestFindByCapability(t *testing.T) {
	b := NewBus()
	b.Register("a1", "A1", "", []string{"search", "read"}, nil)
	b.Register("a2", "A2", "", []string{"write"}, nil)

	matches := b.FindByCapability("search")
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0].ID != "a1" {
		t.Errorf("expected a1, got %q", matches[0].ID)
	}

	noMatch := b.FindByCapability("nonexistent")
	if len(noMatch) != 0 {
		t.Errorf("expected 0 matches, got %d", len(noMatch))
	}
}

func TestSendToNonexistentPeer(t *testing.T) {
	b := NewBus()
	env := &Envelope{
		Type: TypeTaskRequest,
		From: "sender",
		To:   "nobody",
	}
	err := b.Send(context.Background(), env)
	if err == nil {
		t.Error("expected error sending to nonexistent peer")
	}
}

func TestSendClosedBus(t *testing.T) {
	b := NewBus()
	b.Close()

	env := &Envelope{From: "a", To: "b"}
	err := b.Send(context.Background(), env)
	if err == nil {
		t.Error("expected error sending on closed bus")
	}
}

func TestSendBroadcast(t *testing.T) {
	b := NewBus()
	b.Register("sender", "S", "", nil, nil)
	b.Register("recv1", "R1", "", nil, nil)
	b.Register("recv2", "R2", "", nil, nil)

	env := &Envelope{
		Type: TypeBroadcast,
		From: "sender",
		To:   "*",
		Body: json.RawMessage(`"hello"`),
	}
	err := b.Send(context.Background(), env)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSendWithHandler(t *testing.T) {
	b := NewBus()
	responded := make(chan struct{}, 1)

	b.Register("sender", "S", "", nil, nil)
	b.Register("receiver", "R", "", nil, func(ctx context.Context, env *Envelope) (*Envelope, error) {
		responded <- struct{}{}
		return &Envelope{
			Type:    TypeTaskResult,
			Subject: "done",
		}, nil
	})

	env := &Envelope{
		Type: TypeTaskRequest,
		From: "sender",
		To:   "receiver",
		Body: json.RawMessage(`{}`),
	}
	err := b.Send(context.Background(), env)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	select {
	case <-responded:
	case <-time.After(2 * time.Second):
		t.Error("handler not called within timeout")
	}
}

func TestHistory(t *testing.T) {
	b := NewBus()
	b.Register("a", "A", "", nil, nil)
	b.Register("b", "B", "", nil, nil)

	env := &Envelope{
		Type: TypeBroadcast,
		From: "a",
		To:   "*",
	}
	b.Send(context.Background(), env)

	history := b.History()
	if len(history) == 0 {
		t.Error("expected non-empty history after send")
	}
}

func TestDirectChannelBetween(t *testing.T) {
	b := NewBus()
	dc1 := b.DirectChannelBetween("a", "b", 16)
	dc2 := b.DirectChannelBetween("b", "a", 16)

	if dc1 != dc2 {
		t.Error("expected same DirectChannel for same pair regardless of order")
	}
}

func TestBusCloseIdempotent(t *testing.T) {
	b := NewBus()
	b.Close()
	// Should not panic
	b.Close()
}

func TestEnvelopeAutoID(t *testing.T) {
	b := NewBus()
	b.Register("a", "A", "", nil, nil)
	b.Register("b", "B", "", nil, nil)

	env := &Envelope{
		Type: TypeBroadcast,
		From: "a",
		To:   "*",
	}
	if env.ID != "" {
		t.Error("ID should be empty before Send")
	}
	b.Send(context.Background(), env)
	if env.ID == "" {
		t.Error("expected auto-generated ID after Send")
	}
}

func TestPayloadStructs(t *testing.T) {
	task := TaskPayload{
		Description: "do something",
		Input:       map[string]any{"key": "val"},
		Constraints: []string{"no harm"},
	}
	if task.Description == "" {
		t.Error("TaskPayload.Description should not be empty")
	}
	if task.Input["key"] != "val" {
		t.Errorf("TaskPayload.Input: %v", task.Input)
	}
	if len(task.Constraints) != 1 || task.Constraints[0] != "no harm" {
		t.Errorf("TaskPayload.Constraints: %v", task.Constraints)
	}

	result := ResultPayload{
		TaskID:  "t1",
		Success: true,
		Summary: "done",
	}
	if result.TaskID != "t1" {
		t.Errorf("ResultPayload.TaskID: %q", result.TaskID)
	}
	if !result.Success {
		t.Error("ResultPayload.Success should be true")
	}
	if result.Summary != "done" {
		t.Errorf("ResultPayload.Summary: %q", result.Summary)
	}

	status := StatusPayload{
		TaskID:   "t1",
		Progress: 50.0,
		Message:  "halfway",
	}
	if status.TaskID != "t1" {
		t.Errorf("StatusPayload.TaskID: %q", status.TaskID)
	}
	if status.Progress != 50.0 {
		t.Errorf("StatusPayload.Progress: %v", status.Progress)
	}
	if status.Message != "halfway" {
		t.Errorf("StatusPayload.Message: %q", status.Message)
	}

	handoff := HandoffPayload{
		Reason: "escalate",
		Conversation: []ChatMessage{
			{Role: "user", Content: "help me"},
		},
	}
	if handoff.Reason == "" {
		t.Error("HandoffPayload.Reason should not be empty")
	}
	if len(handoff.Conversation) != 1 || handoff.Conversation[0].Content != "help me" {
		t.Errorf("HandoffPayload.Conversation: %+v", handoff.Conversation)
	}
}

func TestHistoryCapEviction(t *testing.T) {
	b := NewBusWithConfig(BusConfig{InboxSize: 100, HistoryCap: 8})
	b.Register("a", "A", "", nil, nil)
	b.Register("b", "B", "", nil, nil)

	for i := 0; i < 12; i++ {
		env := &Envelope{From: "a", To: "*", Type: TypeBroadcast}
		b.Send(context.Background(), env)
	}

	h := b.History()
	if len(h) > 8 {
		t.Errorf("expected history <= 8, got %d", len(h))
	}
}
