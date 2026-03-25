package team

import (
	"context"
	"testing"

	"github.com/spawn08/chronos/sdk/protocol"
)

func TestHandoffResult_UnmarshalTypeError(t *testing.T) {
	_, _, err := HandoffResult(map[string]any{"agent_id": 42, "response": "x"})
	if err == nil {
		t.Fatal("expected unmarshal error when agent_id is numeric in JSON")
	}
}

func TestSetCoordinator_RegistersOnBus(t *testing.T) {
	tm := New("t1", "T", StrategyCoordinator)
	worker := newMockAgent("w1", "worker-reply")
	coord := newMockAgent("c1", "coord")
	tm.AddAgent(worker)
	tm.SetCoordinator(coord)

	ctx := context.Background()
	res, err := tm.Bus.DelegateTask(ctx, "c1", "w1", "sub", protocol.TaskPayload{
		Description: "d",
		Input:       map[string]any{"message": "hi"},
	})
	if err != nil {
		t.Fatalf("delegate: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got err %q", res.Error)
	}
}
