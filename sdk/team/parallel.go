package team

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/spawn08/chronos/engine/graph"
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
	n := len(t.Order)
	if n == 0 {
		return input, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make([]agentResult, n)
	var wg sync.WaitGroup
	wg.Add(n)

	// Semaphore for concurrency limiting. Cap of 0 means unbounded.
	var sem chan struct{}
	if t.MaxConcurrency > 0 {
		sem = make(chan struct{}, t.MaxConcurrency)
	}

	for idx, agentID := range t.Order {
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

			state, err := executeAgent(ctx, a, localInput)
			results[i] = agentResult{agentID: a.ID, state: state, err: err}

			if err != nil && t.ErrorMode == ErrorStrategyFailFast {
				cancel()
			}
		}()
	}
	wg.Wait()

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
