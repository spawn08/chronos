package team

import (
	"context"
	"fmt"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/protocol"
)

// SwarmConfig configures a swarm-style team where agents hand off directly
// to each other without a central coordinator.
type SwarmConfig struct {
	// Agents is the set of agents in the swarm.
	Agents []*agent.Agent
	// InitialAgent is the agent that handles the first message.
	InitialAgent string
	// MaxHandoffs limits the total number of handoffs (0 = 10 default).
	MaxHandoffs int
}

// NewSwarm creates a swarm team where any agent can hand off to any other agent.
// Each agent receives handoff tools for all other agents in the swarm.
func NewSwarm(cfg SwarmConfig) (*Team, error) {
	if len(cfg.Agents) < 2 {
		return nil, fmt.Errorf("swarm: at least 2 agents required")
	}
	if cfg.MaxHandoffs <= 0 {
		cfg.MaxHandoffs = 10
	}

	agentMap := make(map[string]*agent.Agent, len(cfg.Agents))
	for _, a := range cfg.Agents {
		agentMap[a.ID] = a
	}

	if cfg.InitialAgent == "" {
		cfg.InitialAgent = cfg.Agents[0].ID
	}
	if _, ok := agentMap[cfg.InitialAgent]; !ok {
		return nil, fmt.Errorf("swarm: initial agent %q not found", cfg.InitialAgent)
	}

	// Wire handoff tools into each agent
	for _, a := range cfg.Agents {
		for _, target := range cfg.Agents {
			if a.ID == target.ID {
				continue
			}
			handoffTool := NewHandoffTool(HandoffConfig{
				TargetAgent: target,
				Description: fmt.Sprintf("Hand off to %s: %s", target.Name, target.SystemPrompt),
			})
			a.Tools.Register(handoffTool)
		}
	}

	// Build graph: each agent is a node, handoff tools create dynamic edges
	g := graph.New("swarm")
	for _, a := range cfg.Agents {
		agentCopy := a
		g.AddNode(a.ID, func(ctx context.Context, state graph.State) (graph.State, error) {
			input, _ := state["input"].(string)
			resp, err := agentCopy.Chat(ctx, input)
			if err != nil {
				return state, fmt.Errorf("swarm agent %q: %w", agentCopy.ID, err)
			}
			state["output"] = resp.Content
			state["last_agent"] = agentCopy.ID

			// Check if any handoff tool was called
			for _, tc := range resp.ToolCalls {
				for _, other := range cfg.Agents {
					if tc.Name == fmt.Sprintf("transfer_to_%s", other.ID) {
						state["handoff_target"] = other.ID
						state["input"] = tc.Arguments // pass context to next agent
						return state, nil
					}
				}
			}

			state["handoff_target"] = ""
			return state, nil
		})
	}

	g.SetEntryPoint(cfg.InitialAgent)

	// Add conditional edges: if handoff_target is set, route there; otherwise end
	for _, a := range cfg.Agents {
		g.AddConditionalEdge(a.ID, func(state graph.State) string {
			target, _ := state["handoff_target"].(string)
			handoffs, _ := state["handoff_count"].(int)
			if target != "" && handoffs < cfg.MaxHandoffs {
				state["handoff_count"] = handoffs + 1
				return target
			}
			return graph.EndNode
		})
	}

	compiled, err := g.Compile()
	if err != nil {
		return nil, fmt.Errorf("swarm compile: %w", err)
	}

	order := make([]string, 0, len(cfg.Agents))
	for _, a := range cfg.Agents {
		order = append(order, a.ID)
	}

	return &Team{
		ID:            "swarm",
		Name:          "Swarm",
		Strategy:      StrategySwarm,
		Agents:        agentMap,
		Order:         order,
		CompiledGraph: compiled,
		Bus:           protocol.NewBus(),
		SharedContext: make(map[string]any),
		MaxIterations: cfg.MaxHandoffs,
	}, nil
}

// SwarmHandoffTool creates a tool for peer-to-peer handoff in a swarm.
// Unlike the standard handoff tool, this one includes the full task context.
func SwarmHandoffTool(targetID, targetName, description string) *tool.Definition {
	return &tool.Definition{
		Name:        fmt.Sprintf("transfer_to_%s", targetID),
		Description: description,
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task": map[string]any{
					"type":        "string",
					"description": "Description of the task to hand off",
				},
				"context": map[string]any{
					"type":        "string",
					"description": "Relevant context from the current conversation",
				},
			},
			"required": []string{"task"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			return map[string]any{
				"handoff_to": targetID,
				"task":       args["task"],
				"context":    args["context"],
			}, nil
		},
	}
}
