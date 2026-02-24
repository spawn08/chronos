package team

import (
	"context"
	"fmt"

	"github.com/spawn08/chronos/engine/graph"
)

// runRouter dispatches the input to a single agent selected by the routing function.
//
// Two routing modes are supported:
//  1. Static RouterFunc — a pure function that inspects state and returns an agent ID.
//     Fast, deterministic, no LLM call.
//  2. ModelRouterFunc — uses an LLM to reason about which agent is best suited
//     for the task, given agent descriptions and capabilities. More flexible
//     but incurs a model call.
//
// ModelRouter takes precedence when both are set. If neither is set, the router
// selects the agent whose capabilities best match the state keys (simple heuristic).
func (t *Team) runRouter(ctx context.Context, state graph.State) (graph.State, error) {
	agentID, err := t.selectAgent(ctx, state)
	if err != nil {
		return nil, fmt.Errorf("team %q: routing: %w", t.ID, err)
	}

	a, ok := t.Agents[agentID]
	if !ok {
		return nil, fmt.Errorf("team %q: router selected unknown agent %q", t.ID, agentID)
	}

	result, err := executeAgent(ctx, a, state)
	if err != nil {
		return nil, fmt.Errorf("team %q: agent %q: %w", t.ID, agentID, err)
	}
	return result, nil
}

func (t *Team) selectAgent(ctx context.Context, state graph.State) (string, error) {
	if t.ModelRouter != nil {
		return t.ModelRouter(ctx, state, t.agentInfoList())
	}
	if t.Router != nil {
		id := t.Router(state)
		if id == "" {
			return "", fmt.Errorf("RouterFunc returned empty agent ID")
		}
		return id, nil
	}

	// Fallback: capability-based matching.
	return t.capabilityMatch(state)
}

// capabilityMatch scores each agent based on how many of its advertised
// capabilities appear as keys or values in the state. Ties broken by insertion order.
func (t *Team) capabilityMatch(state graph.State) (string, error) {
	if len(t.Order) == 0 {
		return "", fmt.Errorf("no agents registered")
	}

	bestID := t.Order[0]
	bestScore := -1

	for _, id := range t.Order {
		a := t.Agents[id]
		score := 0
		for _, cap := range a.Capabilities {
			if _, ok := state[cap]; ok {
				score += 2
			}
			for _, v := range state {
				if s, ok := v.(string); ok && s == cap {
					score++
				}
			}
		}
		if score > bestScore {
			bestScore = score
			bestID = id
		}
	}
	return bestID, nil
}
