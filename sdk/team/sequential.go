package team

import (
	"context"
	"fmt"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/sdk/agent"
)

// SequentialCheckpoint is called after an agent response and before admitting
// its successor. Durable callers must persist the response under their live
// owner lease; a failed checkpoint stops the team immediately.
type SequentialCheckpoint func(context.Context, int, string, string) error

// RunSequentialWithCheckpoints resumes after an already persisted prefix.
// It never resubmits an agent whose response is in completedResponses. Other
// strategies require their own durable transition protocol.
func (t *Team) RunSequentialWithCheckpoints(ctx context.Context, state graph.State, completedResponses []string, checkpoint SequentialCheckpoint) (graph.State, error) {
	if t.Strategy != StrategySequential || len(completedResponses) > len(t.Order) || checkpoint == nil {
		return nil, fmt.Errorf("team %q: invalid sequential checkpoint request", t.ID)
	}
	return t.runSequentialFrom(ctx, state, completedResponses, checkpoint)
}

// runSequential executes agents as a pipeline: each agent receives the output
// of the previous one. State flows linearly with zero unnecessary copies.
//
// The implementation avoids allocating a new map per step; it reuses the same
// state object, only adding/overwriting keys that the agent produces. Shared
// context is injected lazily — only keys not already present in the current
// state are copied in.
func (t *Team) runSequential(ctx context.Context, state graph.State) (graph.State, error) {
	return t.runSequentialFrom(ctx, state, nil, nil)
}

func (t *Team) runSequentialFrom(ctx context.Context, state graph.State, completed []string, checkpoint SequentialCheckpoint) (graph.State, error) {
	current := state
	for i, agentID := range t.Order {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("team %q: canceled before agent %q: %w", t.ID, agentID, err)
		}

		a, ok := t.Agents[agentID]
		if !ok {
			return nil, fmt.Errorf("team %q: agent %q not found", t.ID, agentID)
		}

		// Inject shared context for keys the current state doesn't have.
		t.sharedMu.RLock()
		for k, v := range t.SharedContext {
			if _, exists := current[k]; !exists {
				current[k] = v
			}
		}
		t.sharedMu.RUnlock()

		// For non-first agents, include the previous agent's response as context.
		if i > 0 {
			if resp, ok := current["response"].(string); ok {
				current["_previous_response"] = resp
			}
		}

		var result graph.State
		if i < len(completed) {
			result = make(graph.State, len(current)+1)
			for key, value := range current {
				result[key] = value
			}
			result["response"] = completed[i]
		} else {
			runCtx := ctx
			if checkpoint != nil {
				if parent, ok := agent.RunIdentityFromContext(ctx); ok {
					child := parent
					child.ParentInvocationID = parent.InvocationID
					child.InvocationID = fmt.Sprintf("%s/team/%s/%d", parent.InvocationID, t.ID, i)
					child.NodeID = fmt.Sprintf("team:%s:%d", t.ID, i)
					child.RoleID = a.ID
					runCtx = agent.WithRunIdentity(ctx, child)
				}
			}
			var err error
			result, err = executeAgent(runCtx, a, current)
			if err != nil {
				return nil, fmt.Errorf("team %q: agent %q (step %d/%d): %w",
					t.ID, a.ID, i+1, len(t.Order), err)
			}
			if checkpoint != nil {
				response, ok := result["response"].(string)
				if !ok {
					return nil, fmt.Errorf("team %q: agent %q returned no text receipt", t.ID, a.ID)
				}
				if err := checkpoint(ctx, i, a.ID, response); err != nil {
					return nil, fmt.Errorf("team %q: checkpoint agent %q: %w", t.ID, a.ID, err)
				}
			}
		}

		// Merge result into current state in-place.
		for k, v := range result {
			current[k] = v
		}

		// Accumulate into shared context for future steps and broadcasts.
		t.sharedMu.Lock()
		for k, v := range result {
			t.SharedContext[k] = v
		}
		t.sharedMu.Unlock()

		// Lightweight broadcast — fire-and-forget, non-blocking.
		_ = t.Broadcast(ctx, a.ID, fmt.Sprintf("step:%d:completed", i+1), current)
	}
	return current, nil
}
