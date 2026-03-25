package team

import (
	"context"
	"testing"

	"github.com/spawn08/chronos/sdk/agent"
)

func TestCreateHandoffTools(t *testing.T) {
	a1 := newMockAgent("a1", "response from a1")
	a2 := newMockAgent("a2", "response from a2")
	a3 := newMockAgent("a3", "response from a3")

	tools := CreateHandoffTools([]*agent.Agent{a1, a2, a3})
	// Each agent should have tools for the other 2
	if len(tools["a1"]) != 2 {
		t.Errorf("a1 expected 2 handoff tools, got %d", len(tools["a1"]))
	}
	if len(tools["a2"]) != 2 {
		t.Errorf("a2 expected 2 handoff tools, got %d", len(tools["a2"]))
	}
	// No self-transfer
	for _, def := range tools["a1"] {
		if def.Name == "transfer_to_a1" {
			t.Error("a1 should not have a transfer to itself")
		}
	}
}

func TestCreateHandoffTools_TwoAgents(t *testing.T) {
	a1 := newMockAgent("x", "rx")
	a2 := newMockAgent("y", "ry")
	tools := CreateHandoffTools([]*agent.Agent{a1, a2})

	if len(tools["x"]) != 1 {
		t.Errorf("expected 1 tool for x, got %d", len(tools["x"]))
	}
	if tools["x"][0].Name != "transfer_to_y" {
		t.Errorf("expected transfer_to_y, got %q", tools["x"][0].Name)
	}
}

func TestHandoffResult_Valid(t *testing.T) {
	result := map[string]any{
		"agent_id":   "analyst",
		"agent_name": "Analyst",
		"response":   "analysis complete",
	}
	agentID, response, err := HandoffResult(result)
	if err != nil {
		t.Fatalf("HandoffResult: %v", err)
	}
	if agentID != "analyst" {
		t.Errorf("agentID=%q", agentID)
	}
	if response != "analysis complete" {
		t.Errorf("response=%q", response)
	}
}

func TestHandoffResult_Invalid(t *testing.T) {
	// Pass something that can't be marshaled normally — use a channel
	_, _, err := HandoffResult(make(chan int))
	if err == nil {
		t.Fatal("expected error for unmarshalable type")
	}
}

func TestNewHandoffTool_WithInstructions(t *testing.T) {
	target := newMockAgent("target", "done")
	def := NewHandoffTool(HandoffConfig{
		TargetAgent:  target,
		Description:  "Custom description",
		Instructions: "Follow these instructions",
	})
	if def == nil {
		t.Fatal("expected non-nil definition")
	}
	if def.Name != "transfer_to_target" {
		t.Errorf("Name=%q", def.Name)
	}
	if def.Description != "Custom description" {
		t.Errorf("Description=%q", def.Description)
	}
	// Test handler with message
	result, err := def.Handler(context.Background(), map[string]any{"message": "do it"})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestNewHandoffTool_NoMessage(t *testing.T) {
	target := newMockAgent("target2", "done")
	def := NewHandoffTool(HandoffConfig{TargetAgent: target})
	// Handler with empty message should use default
	result, err := def.Handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}
