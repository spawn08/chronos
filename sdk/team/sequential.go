package team

import (
	"context"
	"fmt"

	"github.com/spawn08/chronos/engine/graph"
)

// runSequential executes agents as a pipeline: each agent receives the output
// of the previous one. State flows linearly with zero unnecessary copies.
//
// The implementation avoids allocating a new map per step; it reuses the same
// state object, only adding/overwriting keys that the agent produces. Shared
// context is injected lazily — only keys not already present in the current
// state are copied in.
func (t *Team) runSequential(ctx context.Context, state graph.State) (graph.State, error) {
	current := state
	for i, agentID := range t.Order {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("team %q: cancelled before agent %q: %w", t.ID, agentID, err)
		}

		a, ok := t.Agents[agentID]
		if !ok {
			return nil, fmt.Errorf("team %q: agent %q not found", t.ID, agentID)
		}

		// Inject shared context for keys the current state doesn't have.
		for k, v := range t.SharedContext {
			if _, exists := current[k]; !exists {
				current[k] = v
			}
		}

		// For non-first agents, include the previous agent's response as context.
		if i > 0 {
			if resp, ok := current["response"].(string); ok {
				current["_previous_response"] = resp
			}
		}

		result, err := executeAgent(ctx, a, current)
		if err != nil {
			return nil, fmt.Errorf("team %q: agent %q (step %d/%d): %w",
				t.ID, a.ID, i+1, len(t.Order), err)
		}

		// Merge result into current state in-place.
		for k, v := range result {
			current[k] = v
		}

		// Accumulate into shared context for future steps and broadcasts.
		for k, v := range result {
			t.SharedContext[k] = v
		}

		// Lightweight broadcast — fire-and-forget, non-blocking.
		_ = t.Broadcast(ctx, a.ID, fmt.Sprintf("step:%d:completed", i+1), current)
	}
	return current, nil
}
