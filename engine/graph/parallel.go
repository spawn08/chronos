package graph

import (
	"context"
	"fmt"
	"sync"
)

// FanOut creates a NodeFunc that executes multiple node functions in parallel.
// Each function receives a copy of the input state.
// Results are merged back using a MergeFunc.
func FanOut(branches []NodeFunc, merge MergeFunc) NodeFunc {
	return func(ctx context.Context, state State) (State, error) {
		type result struct {
			state State
			err   error
		}

		results := make([]result, len(branches))
		var wg sync.WaitGroup
		wg.Add(len(branches))

		for i, fn := range branches {
			go func(idx int, f NodeFunc) {
				defer wg.Done()
				cp := copyState(state)
				s, err := f(ctx, cp)
				results[idx] = result{state: s, err: err}
			}(i, fn)
		}
		wg.Wait()

		var states []State
		for i, r := range results {
			if r.err != nil {
				return state, fmt.Errorf("fan-out branch %d: %w", i, r.err)
			}
			states = append(states, r.state)
		}

		merged, err := merge(state, states)
		if err != nil {
			return state, fmt.Errorf("fan-in merge: %w", err)
		}
		return merged, nil
	}
}

// MergeFunc combines the results of parallel branches into a single state.
// It receives the original pre-fork state and the results from each branch.
type MergeFunc func(original State, results []State) (State, error)

// MergeAll is a simple merge strategy that applies all branch results
// on top of the original state. Later branches overwrite earlier ones
// for conflicting keys.
func MergeAll(original State, results []State) (State, error) {
	merged := copyState(original)
	for _, s := range results {
		for k, v := range s {
			merged[k] = v
		}
	}
	return merged, nil
}

func copyState(s State) State {
	cp := make(State, len(s))
	for k, v := range s {
		cp[k] = v
	}
	return cp
}
