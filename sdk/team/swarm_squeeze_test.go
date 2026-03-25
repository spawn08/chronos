package team

import (
	"testing"

	"github.com/spawn08/chronos/sdk/agent"
)

func TestNewSwarm_TooFewAgents_Squeeze(t *testing.T) {
	t.Parallel()
	only, err := agent.New("only", "Only").Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	_, err = NewSwarm(SwarmConfig{Agents: []*agent.Agent{only}})
	if err == nil {
		t.Fatal("expected error for single agent")
	}
}

func TestNewSwarm_InitialAgentMissing_Squeeze(t *testing.T) {
	t.Parallel()
	a1, err := agent.New("a1", "A1").Build()
	if err != nil {
		t.Fatalf("Build a1: %v", err)
	}
	a2, err := agent.New("a2", "A2").Build()
	if err != nil {
		t.Fatalf("Build a2: %v", err)
	}
	_, err = NewSwarm(SwarmConfig{
		Agents:       []*agent.Agent{a1, a2},
		InitialAgent: "nope",
		MaxHandoffs:  3,
	})
	if err == nil {
		t.Fatal("expected error for unknown initial agent")
	}
}

func TestNewSwarm_DefaultMaxHandoffs_Squeeze(t *testing.T) {
	t.Parallel()
	a1, err := agent.New("a1", "A1").Build()
	if err != nil {
		t.Fatalf("Build a1: %v", err)
	}
	a2, err := agent.New("a2", "A2").Build()
	if err != nil {
		t.Fatalf("Build a2: %v", err)
	}
	tm, err := NewSwarm(SwarmConfig{Agents: []*agent.Agent{a1, a2}})
	if err != nil {
		t.Fatalf("NewSwarm: %v", err)
	}
	if tm == nil {
		t.Fatal("nil team")
	}
}
