package team

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/model"
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

// NewModelRouter returns a ModelRouterFunc that asks an LLM to pick the single
// best-suited agent for the task described in the state.
//
// The router builds a system prompt from the agents' IDs, names, descriptions,
// and capabilities, sends the task (state["message"], or a flattened view of the
// state) as the user turn, and requests a JSON object of the form
// {"agent_id": "<id>"}. The returned ID is validated against the supplied agent
// list; if the model answers with prose or an unknown ID, the router attempts to
// recover the choice by scanning the response for a known agent ID before
// failing. Temperature is pinned to 0 for deterministic dispatch.
//
// Pair it with StrategyRouter via Team.SetModelRouter. Unlike the capability
// heuristic (the zero-config fallback), this makes routing decisions from the
// task's actual meaning rather than string overlap with state keys.
func NewModelRouter(provider model.Provider) ModelRouterFunc {
	return func(ctx context.Context, state graph.State, agents []AgentInfo) (string, error) {
		if provider == nil {
			return "", fmt.Errorf("model router: nil provider")
		}
		if len(agents) == 0 {
			return "", fmt.Errorf("model router: no agents to route to")
		}

		task, _ := state["message"].(string)
		if task == "" {
			task = stateToPrompt(state)
		}

		req := &model.ChatRequest{
			Messages: []model.Message{
				{Role: model.RoleSystem, Content: routerSystemPrompt(agents)},
				{Role: model.RoleUser, Content: task},
			},
			ResponseFormat: "json_object",
			Temperature:    0,
		}

		resp, err := provider.Chat(ctx, req)
		if err != nil {
			return "", fmt.Errorf("model router: chat: %w", err)
		}

		id, err := parseRouterChoice(resp.Content, agents)
		if err != nil {
			return "", fmt.Errorf("model router: %w", err)
		}
		return id, nil
	}
}

// routerSystemPrompt describes the roster and instructs the model to return a
// single agent ID as JSON.
func routerSystemPrompt(agents []AgentInfo) string {
	var b strings.Builder
	b.WriteString("You are a router that dispatches a task to exactly one specialist agent.\n\n")
	b.WriteString("Available agents:\n")
	for _, a := range agents {
		b.WriteString(fmt.Sprintf("- ID: %q, Name: %q, Description: %q", a.ID, a.Name, a.Description))
		if len(a.Capabilities) > 0 {
			b.WriteString(fmt.Sprintf(", Capabilities: %v", a.Capabilities))
		}
		b.WriteString("\n")
	}
	b.WriteString("\nChoose the single agent best suited to handle the task. ")
	b.WriteString(`Respond with a JSON object of the form {"agent_id": "<id>"} using one of the IDs above and nothing else.`)
	return b.String()
}

// routerChoice is the structured selection the router LLM produces.
type routerChoice struct {
	AgentID string `json:"agent_id"`
}

// parseRouterChoice extracts a valid agent ID from the model's response. It first
// tries strict JSON, then a JSON object embedded in surrounding text, and finally
// scans for any known agent ID mentioned in the content.
func parseRouterChoice(content string, agents []AgentInfo) (string, error) {
	valid := make(map[string]bool, len(agents))
	for _, a := range agents {
		valid[a.ID] = true
	}

	var choice routerChoice
	if err := json.Unmarshal([]byte(content), &choice); err != nil {
		_ = json.Unmarshal([]byte(extractJSON(content)), &choice)
	}
	if choice.AgentID != "" && valid[choice.AgentID] {
		return choice.AgentID, nil
	}

	// Recovery: the model may have answered with prose. Pick the first known
	// agent ID that appears in the response.
	for _, a := range agents {
		if strings.Contains(content, a.ID) {
			return a.ID, nil
		}
	}

	if choice.AgentID != "" {
		return "", fmt.Errorf("model chose unknown agent %q", choice.AgentID)
	}
	return "", fmt.Errorf("model returned no usable agent ID (raw: %s)", strings.TrimSpace(content))
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
