package team

import (
	"testing"

	"github.com/spawn08/chronos/sdk/agent"
)

func TestNewSwarm_ExplicitPositiveMaxHandoffs_Push(t *testing.T) {
	a1 := newMockAgent("a1", "r1")
	a2 := newMockAgent("a2", "r2")
	tm, err := NewSwarm(SwarmConfig{
		Agents:       []*agent.Agent{a1, a2},
		InitialAgent: "a1",
		MaxHandoffs:  4,
	})
	if err != nil {
		t.Fatalf("NewSwarm: %v", err)
	}
	if tm == nil || tm.Agents["a1"] == nil {
		t.Fatal("expected team with agents")
	}
}

func TestNewSwarm_InitialAgentIsSecond_Push(t *testing.T) {
	a1 := newMockAgent("a1", "r1")
	a2 := newMockAgent("a2", "r2")
	tm, err := NewSwarm(SwarmConfig{
		Agents:       []*agent.Agent{a1, a2},
		InitialAgent: "a2",
	})
	if err != nil {
		t.Fatalf("NewSwarm: %v", err)
	}
	if tm.Strategy != "swarm" || tm.Agents["a2"] == nil {
		t.Fatalf("unexpected team: %+v", tm)
	}
}

func TestNewSwarm_DuplicateAgentIDs_Push(t *testing.T) {
	// Two distinct agent structs with the same ID: map collapses to one entry,
	// graph has a single node; handoff tools are skipped for same ID pairs.
	a1 := newMockAgent("dup", "r1")
	a2 := newMockAgent("dup", "r2")
	tm, err := NewSwarm(SwarmConfig{Agents: []*agent.Agent{a1, a2}})
	if err != nil {
		t.Fatalf("NewSwarm: %v", err)
	}
	if tm == nil {
		t.Fatal("expected non-nil team")
	}
}
