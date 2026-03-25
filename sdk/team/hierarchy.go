package team

import (
	"context"
	"fmt"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/protocol"
)

// HierarchyConfig configures a hierarchical multi-level supervisor team.
type HierarchyConfig struct {
	// Root is the top-level supervisor.
	Root *SupervisorNode
}

// SupervisorNode represents a node in the supervisor hierarchy.
// It can contain either agents (leaf workers) or other supervisors (sub-teams).
type SupervisorNode struct {
	// Supervisor is the agent that manages this level.
	Supervisor *agent.Agent
	// Workers are leaf-level agents at this level.
	Workers []*agent.Agent
	// SubTeams are nested supervisor nodes.
	SubTeams []*SupervisorNode
}

// NewHierarchy creates a hierarchical team where supervisors delegate to
// mid-level supervisors, which delegate to worker agents.
func NewHierarchy(cfg HierarchyConfig) (*Team, error) {
	if cfg.Root == nil {
		return nil, fmt.Errorf("hierarchy: root supervisor is required")
	}
	if cfg.Root.Supervisor == nil {
		return nil, fmt.Errorf("hierarchy: root supervisor agent is required")
	}

	agentList, err := collectAgents(cfg.Root)
	if err != nil {
		return nil, fmt.Errorf("hierarchy: %w", err)
	}

	agentMap := make(map[string]*agent.Agent, len(agentList))
	for _, a := range agentList {
		agentMap[a.ID] = a
	}

	g := graph.New("hierarchy")
	buildHierarchyGraph(g, cfg.Root)

	g.SetEntryPoint(cfg.Root.Supervisor.ID)

	compiled, err := g.Compile()
	if err != nil {
		return nil, fmt.Errorf("hierarchy compile: %w", err)
	}

	order := make([]string, 0, len(agentList))
	for _, a := range agentList {
		order = append(order, a.ID)
	}

	return &Team{
		ID:            "hierarchy",
		Name:          "Hierarchy",
		Strategy:      StrategyHierarchy,
		Agents:        agentMap,
		Order:         order,
		CompiledGraph: compiled,
		Bus:           protocol.NewBus(),
		SharedContext: make(map[string]any),
		MaxIterations: 1,
	}, nil
}

func buildHierarchyGraph(g *graph.StateGraph, node *SupervisorNode) {
	sup := node.Supervisor

	// Add supervisor node
	g.AddNode(sup.ID, func(ctx context.Context, state graph.State) (graph.State, error) {
		input, _ := state["input"].(string)
		resp, err := sup.Chat(ctx, input)
		if err != nil {
			return state, fmt.Errorf("supervisor %q: %w", sup.ID, err)
		}
		state["output"] = resp.Content
		state[sup.ID+"_output"] = resp.Content
		return state, nil
	})

	// Add worker nodes
	for _, worker := range node.Workers {
		workerCopy := worker
		g.AddNode(worker.ID, func(ctx context.Context, state graph.State) (graph.State, error) {
			input, _ := state["input"].(string)
			resp, err := workerCopy.Chat(ctx, input)
			if err != nil {
				return state, fmt.Errorf("worker %q: %w", workerCopy.ID, err)
			}
			state[workerCopy.ID+"_output"] = resp.Content
			state["output"] = resp.Content
			return state, nil
		})
		g.AddEdge(sup.ID, worker.ID)
		g.AddEdge(worker.ID, graph.EndNode)
	}

	// Recursively build sub-team nodes
	for _, sub := range node.SubTeams {
		buildHierarchyGraph(g, sub)
		g.AddEdge(sup.ID, sub.Supervisor.ID)
	}

	// If no workers or sub-teams, this supervisor is a leaf
	if len(node.Workers) == 0 && len(node.SubTeams) == 0 {
		g.SetFinishPoint(sup.ID)
	}
}

func collectAgents(node *SupervisorNode) ([]*agent.Agent, error) {
	var agents []*agent.Agent
	if node.Supervisor == nil {
		return nil, fmt.Errorf("supervisor node missing agent")
	}
	agents = append(agents, node.Supervisor)
	agents = append(agents, node.Workers...)
	for _, sub := range node.SubTeams {
		subAgents, err := collectAgents(sub)
		if err != nil {
			return nil, err
		}
		agents = append(agents, subAgents...)
	}
	return agents, nil
}
