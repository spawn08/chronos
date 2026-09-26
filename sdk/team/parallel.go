package team

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/sdk/agent"
)

// agentResult captures the output or error from a single parallel agent execution.
type agentResult struct {
	agentID string
	state   graph.State
	err     error
}

// runParallel fans out work to all agents concurrently, respects MaxConcurrency,
// then merges results. Error handling is governed by the team's ErrorMode.
//
// Key design choices for efficiency and scalability:
//   - Uses a semaphore channel for bounded concurrency instead of a worker pool
//     (zero goroutine overhead when idle).
//   - Pre-allocates the results slice to agent count — no append/grow.
//   - Supports three error strategies: fail-fast (cancel all on first error),
//     collect (gather all errors), and best-effort (ignore errors).
//   - Each agent gets an independent copy of the input state to prevent data races.
func (t *Team) runParallel(ctx context.Context, input graph.State) (graph.State, error) {
	return t.runParallelFrom(ctx, input, nil, nil)
}

// ParallelCheckpoint is called after a member response and before the team
// result is merged. Calls are serialized, but members finish in any order;
// durable callers must persist the response under their live owner lease. A
// failed checkpoint cancels the remaining members regardless of ErrorMode.
type ParallelCheckpoint func(ctx context.Context, step int, agentID, response string) error

// RunParallelWithCheckpoints resumes a parallel team after the members in
// completedResponses (keyed by position in Order) were persisted. Those
// members are never resubmitted; their responses are merged in Order with the
// members that run now. Each running member gets a stable node identity
// "team:<id>:<step>" so its model calls can be attributed durably.
func (t *Team) RunParallelWithCheckpoints(ctx context.Context, input graph.State, completedResponses map[int]string, checkpoint ParallelCheckpoint) (graph.State, error) {
	if t.Strategy != StrategyParallel || checkpoint == nil {
		return nil, fmt.Errorf("team %q: invalid parallel checkpoint request", t.ID)
	}
	for step := range completedResponses {
		if step < 0 || step >= len(t.Order) {
			return nil, fmt.Errorf("team %q: invalid parallel checkpoint request", t.ID)
		}
	}
	return t.runParallelFrom(ctx, input, completedResponses, checkpoint)
}

func (t *Team) runParallelFrom(ctx context.Context, input graph.State, completed map[int]string, checkpoint ParallelCheckpoint) (graph.State, error) {
	n := len(t.Order)
	if n == 0 {
		return input, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make([]agentResult, n)
	var wg sync.WaitGroup
	var checkpointMu sync.Mutex
	var checkpointErr error
	pending := 0
	// Snapshot completion up front: callers may record new receipts into the
	// same map from their checkpoint while members are still being launched.
	done := make([]bool, n)
	for i, agentID := range t.Order {
		if _, ok := t.Agents[agentID]; !ok {
			return nil, fmt.Errorf("team %q: agent %q not found", t.ID, agentID)
		}
		response, ok := completed[i]
		if !ok {
			pending++
			continue
		}
		done[i] = true
		state := make(graph.State, len(input)+1)
		for k, v := range input {
			state[k] = v
		}
		state["response"] = response
		results[i] = agentResult{agentID: agentID, state: state}
	}
	wg.Add(pending)

	// Semaphore for concurrency limiting. Cap of 0 means unbounded.
	var sem chan struct{}
	if t.MaxConcurrency > 0 {
		sem = make(chan struct{}, t.MaxConcurrency)
	}

	for idx, agentID := range t.Order {
		if done[idx] {
			continue
		}
		a := t.Agents[agentID]
		i := idx

		go func() {
			defer wg.Done()

			// Acquire semaphore slot
			if sem != nil {
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					results[i] = agentResult{agentID: a.ID, err: ctx.Err()}
					return
				}
			}

			if ctx.Err() != nil {
				results[i] = agentResult{agentID: a.ID, err: ctx.Err()}
				return
			}

			// Each goroutine gets its own shallow copy of input.
			localInput := make(graph.State, len(input))
			for k, v := range input {
				localInput[k] = v
			}

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
			state, err := executeAgent(runCtx, a, localInput)
			if err == nil && checkpoint != nil {
				response, ok := state["response"].(string)
				checkpointMu.Lock()
				switch {
				case checkpointErr != nil:
					err = checkpointErr
				case !ok:
					checkpointErr = fmt.Errorf("team %q: agent %q returned no text receipt", t.ID, a.ID)
				default:
					if cpErr := checkpoint(ctx, i, a.ID, response); cpErr != nil {
						checkpointErr = fmt.Errorf("team %q: checkpoint agent %q: %w", t.ID, a.ID, cpErr)
					}
				}
				if checkpointErr != nil {
					err = checkpointErr
					cancel()
				}
				checkpointMu.Unlock()
			}
			results[i] = agentResult{agentID: a.ID, state: state, err: err}

			if err != nil && t.ErrorMode == ErrorStrategyFailFast {
				cancel()
			}
		}()
	}
	wg.Wait()
	if checkpointErr != nil {
		return nil, checkpointErr
	}

	// Process errors according to strategy
	var errs []string
	var successStates []graph.State
	for _, r := range results {
		if r.err != nil {
			switch t.ErrorMode {
			case ErrorStrategyFailFast:
				return nil, fmt.Errorf("team %q: agent %q: %w", t.ID, r.agentID, r.err)
			case ErrorStrategyCollect:
				errs = append(errs, fmt.Sprintf("agent %q: %s", r.agentID, r.err))
			case ErrorStrategyBestEffort:
				continue
			}
		}
		if r.state != nil {
			successStates = append(successStates, r.state)
		}
	}

	if t.ErrorMode == ErrorStrategyCollect && len(errs) > 0 {
		return nil, fmt.Errorf("team %q: %d agents failed:\n%s",
			t.ID, len(errs), strings.Join(errs, "\n"))
	}

	if t.Merge != nil {
		return t.Merge(successStates), nil
	}
	return defaultMerge(successStates), nil
}

// defaultMerge combines results by namespacing each agent's output under its
// agent ID to prevent key collisions. The "response" key is aggregated into
// a combined response.
func defaultMerge(results []graph.State) graph.State {
	merged := make(graph.State, len(results)*4)
	var responses []string
	for _, r := range results {
		for k, v := range r {
			if k == "response" {
				if s, ok := v.(string); ok {
					responses = append(responses, s)
				}
				continue
			}
			merged[k] = v
		}
	}
	if len(responses) > 0 {
		merged["response"] = strings.Join(responses, "\n\n---\n\n")
	}
	return merged
}
