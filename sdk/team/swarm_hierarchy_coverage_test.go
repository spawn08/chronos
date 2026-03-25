package team

import (
	"context"
	"errors"
	"testing"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
)

func TestCollectAgents_Table(t *testing.T) {
	root := newMockAgent("root", "r")
	w1 := newMockAgent("w1", "w")

	subSup := newMockAgent("subsup", "s")
	subW := newMockAgent("subw", "sw")

	tests := []struct {
		name    string
		node    *SupervisorNode
		wantLen int
		wantErr bool
	}{
		{
			name:    "supervisor_only",
			node:    &SupervisorNode{Supervisor: root},
			wantLen: 1,
		},
		{
			name:    "supervisor_and_workers",
			node:    &SupervisorNode{Supervisor: root, Workers: []*agent.Agent{w1}},
			wantLen: 2,
		},
		{
			name: "nested_subteam",
			node: &SupervisorNode{
				Supervisor: root,
				SubTeams: []*SupervisorNode{
					{Supervisor: subSup, Workers: []*agent.Agent{subW}},
				},
			},
			wantLen: 3,
		},
		{
			name:    "nil_supervisor",
			node:    &SupervisorNode{Supervisor: nil},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := collectAgents(tt.node)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("collectAgents: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("len=%d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestBuildHierarchyGraph_SupervisorNodeError(t *testing.T) {
	g := graph.New("hier-err")
	sup := newMockAgentWithError("sup", errors.New("supervisor chat failed"))
	buildHierarchyGraph(g, &SupervisorNode{Supervisor: sup})
	g.SetEntryPoint(sup.ID)
	g.SetFinishPoint(sup.ID)

	cg, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	fn := cg.Nodes[sup.ID].Fn
	_, err = fn(context.Background(), graph.State{"input": "task"})
	if err == nil {
		t.Fatal("expected error from supervisor node")
	}
}

func TestBuildHierarchyGraph_WorkerNodeError(t *testing.T) {
	g := graph.New("hier-worker-err")
	sup := newMockAgent("sup", "ok")
	worker := newMockAgentWithError("w", errors.New("worker failed"))
	buildHierarchyGraph(g, &SupervisorNode{Supervisor: sup, Workers: []*agent.Agent{worker}})
	g.SetEntryPoint(sup.ID)

	cg, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	wfn := cg.Nodes[worker.ID].Fn
	_, err = wfn(context.Background(), graph.State{"input": "do work"})
	if err == nil {
		t.Fatal("expected error from worker node")
	}
}

func TestBuildHierarchyGraph_SubTeamSupervisorEdge(t *testing.T) {
	// Supervisor with only sub-teams (no direct workers) — edges sup -> sub supervisor
	root := newMockAgent("root", "r")
	sub := newMockAgent("sub", "s")
	g := graph.New("subonly")
	buildHierarchyGraph(g, &SupervisorNode{
		Supervisor: root,
		SubTeams: []*SupervisorNode{
			{Supervisor: sub, Workers: nil, SubTeams: nil},
		},
	})
	g.SetEntryPoint(root.ID)
	_, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
}

type mockToolCallProvider struct {
	content   string
	toolCalls []model.ToolCall
	err       error
}

func (p *mockToolCallProvider) Chat(_ context.Context, _ *model.ChatRequest) (*model.ChatResponse, error) {
	if p.err != nil {
		return nil, p.err
	}
	return &model.ChatResponse{Content: p.content, ToolCalls: p.toolCalls}, nil
}

func (p *mockToolCallProvider) StreamChat(_ context.Context, _ *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, errors.New("not implemented")
}

func (p *mockToolCallProvider) Name() string  { return "mock" }
func (p *mockToolCallProvider) Model() string { return "mock" }

func TestNewSwarm_ConfigVariations(t *testing.T) {
	a1 := newMockAgent("a1", "r1")
	a2 := newMockAgent("a2", "r2")
	a3, _ := agent.New("a3", "a3").WithModel(&mockToolCallProvider{
		content: "handoff",
		toolCalls: []model.ToolCall{
			{Name: "transfer_to_a2", Arguments: "next task"},
		},
	}).Build()

	team, err := NewSwarm(SwarmConfig{
		Agents:       []*agent.Agent{a1, a2, a3},
		InitialAgent: "a2",
		MaxHandoffs:  3,
	})
	if err != nil {
		t.Fatalf("NewSwarm: %v", err)
	}
	if team.Agents["a2"] == nil {
		t.Fatal("missing initial agent")
	}
}
