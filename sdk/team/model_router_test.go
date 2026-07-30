package team

import (
	"context"
	"errors"
	"testing"

	"github.com/spawn08/chronos/engine/graph"
)

func routerAgents() []AgentInfo {
	return []AgentInfo{
		{ID: "researcher", Name: "Research Analyst", Description: "Gathers facts", Capabilities: []string{"research"}},
		{ID: "writer", Name: "Content Writer", Description: "Writes articles", Capabilities: []string{"writing"}},
		{ID: "editor", Name: "Senior Editor", Description: "Polishes prose", Capabilities: []string{"editing"}},
	}
}

func TestNewModelRouter(t *testing.T) {
	tests := []struct {
		name     string
		response string
		respErr  error
		want     string
		wantErr  bool
	}{
		{name: "strict json", response: `{"agent_id":"writer"}`, want: "writer"},
		{name: "json in prose", response: "Sure! {\"agent_id\": \"editor\"} is best.", want: "editor"},
		{name: "bare id recovery", response: "I would route this to researcher.", want: "researcher"},
		{name: "unknown agent", response: `{"agent_id":"nobody"}`, wantErr: true},
		{name: "empty response", response: "", wantErr: true},
		{name: "provider error", respErr: errors.New("boom"), wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router := NewModelRouter(&mockProvider{response: tc.response, err: tc.respErr})
			got, err := router(context.Background(), graph.State{"message": "write a piece on X"}, routerAgents())
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got id=%q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("agent = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNewModelRouterGuards(t *testing.T) {
	if _, err := NewModelRouter(nil)(context.Background(), graph.State{}, routerAgents()); err == nil {
		t.Error("nil provider should error")
	}
	router := NewModelRouter(&mockProvider{response: `{"agent_id":"writer"}`})
	if _, err := router(context.Background(), graph.State{}, nil); err == nil {
		t.Error("empty agent list should error")
	}
}

// TestRouterStrategyUsesModelRouter verifies a router team dispatches to the
// model-selected agent rather than falling back to the first agent.
func TestRouterStrategyUsesModelRouter(t *testing.T) {
	tm := New("route", "Router", StrategyRouter)
	tm.AddAgent(newMockAgent("researcher", "facts"))
	tm.AddAgent(newMockAgent("writer", "an article"))
	tm.AddAgent(newMockAgent("editor", "polished"))

	tm.SetModelRouter(NewModelRouter(&mockProvider{response: `{"agent_id":"writer"}`}))

	result, err := tm.Run(context.Background(), graph.State{"message": "draft something"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp, _ := result["response"].(string); resp != "an article" {
		t.Errorf("response = %q, want %q (writer should have been selected)", resp, "an article")
	}
}
