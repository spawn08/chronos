package team

import (
	"context"
	"testing"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/sdk/agent"
)

func TestRouter_CapabilityMatchByMessageValue(t *testing.T) {
	a1, err := agent.New("sql-agent", "SQL Agent").
		WithModel(&mockProvider{response: "sql-result"}).
		AddCapability("sql").
		Build()
	if err != nil {
		t.Fatal(err)
	}
	a2, err := agent.New("py-agent", "Python Agent").
		WithModel(&mockProvider{response: "py-result"}).
		AddCapability("python").
		Build()
	if err != nil {
		t.Fatal(err)
	}

	tm := New("cap-router", "Cap Router", StrategyRouter)
	tm.AddAgent(a1)
	tm.AddAgent(a2)

	// State value "sql" matches a1's capability via string equality (score += 1)
	result, err := tm.Run(context.Background(), graph.State{"message": "sql"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	resp, _ := result["response"].(string)
	if resp != "sql-result" {
		t.Errorf("response = %q, want sql-result", resp)
	}
}
