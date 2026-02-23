// Package team provides multi-agent orchestration inspired by Agno's team coordination.
package team

import (
	"context"
	"fmt"
	"sync"

	"github.com/chronos-ai/chronos/engine/graph"
	"github.com/chronos-ai/chronos/sdk/agent"
)

// Strategy defines how work is distributed across team members.
type Strategy string

const (
	// StrategySequential runs agents one after another, passing state along.
	StrategySequential Strategy = "sequential"
	// StrategyParallel runs all agents concurrently and merges results.
	StrategyParallel Strategy = "parallel"
	// StrategyRouter uses a routing function to select which agent handles the input.
	StrategyRouter Strategy = "router"
)

// RouterFunc selects an agent ID based on the current state.
type RouterFunc func(state graph.State) string

// MergeFunc combines results from parallel agent executions.
type MergeFunc func(results []graph.State) graph.State

// Team orchestrates multiple agents working together.
type Team struct {
	ID       string
	Name     string
	Agents   map[string]*agent.Agent
	Strategy Strategy
	Router   RouterFunc
	Merge    MergeFunc
}

// New creates a team with the given strategy.
func New(id, name string, strategy Strategy) *Team {
	return &Team{
		ID:       id,
		Name:     name,
		Agents:   make(map[string]*agent.Agent),
		Strategy: strategy,
	}
}

// AddAgent adds an agent to the team.
func (t *Team) AddAgent(a *agent.Agent) *Team {
	t.Agents[a.ID] = a
	return t
}

// SetRouter sets the routing function (for StrategyRouter).
func (t *Team) SetRouter(fn RouterFunc) *Team {
	t.Router = fn
	return t
}

// SetMerge sets the merge function (for StrategyParallel).
func (t *Team) SetMerge(fn MergeFunc) *Team {
	t.Merge = fn
	return t
}

// Run executes the team's strategy with the given input.
func (t *Team) Run(ctx context.Context, input graph.State) (graph.State, error) {
	switch t.Strategy {
	case StrategySequential:
		return t.runSequential(ctx, input)
	case StrategyParallel:
		return t.runParallel(ctx, input)
	case StrategyRouter:
		return t.runRouter(ctx, input)
	default:
		return nil, fmt.Errorf("team %q: unknown strategy %q", t.ID, t.Strategy)
	}
}

func (t *Team) runSequential(ctx context.Context, state graph.State) (graph.State, error) {
	current := state
	for _, a := range t.Agents {
		result, err := a.Run(ctx, current)
		if err != nil {
			return nil, fmt.Errorf("team %q: agent %q: %w", t.ID, a.ID, err)
		}
		current = graph.State(result.State)
	}
	return current, nil
}

func (t *Team) runParallel(ctx context.Context, input graph.State) (graph.State, error) {
	var mu sync.Mutex
	var results []graph.State
	var firstErr error

	var wg sync.WaitGroup
	for _, a := range t.Agents {
		wg.Add(1)
		go func(ag *agent.Agent) {
			defer wg.Done()
			result, err := ag.Run(ctx, input)
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("team %q: agent %q: %w", t.ID, ag.ID, err)
				return
			}
			if result != nil {
				results = append(results, graph.State(result.State))
			}
		}(a)
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	if t.Merge != nil {
		return t.Merge(results), nil
	}
	// Default merge: combine all keys, last writer wins
	merged := make(graph.State)
	for _, r := range results {
		for k, v := range r {
			merged[k] = v
		}
	}
	return merged, nil
}

func (t *Team) runRouter(ctx context.Context, state graph.State) (graph.State, error) {
	if t.Router == nil {
		return nil, fmt.Errorf("team %q: router strategy requires a RouterFunc", t.ID)
	}
	agentID := t.Router(state)
	a, ok := t.Agents[agentID]
	if !ok {
		return nil, fmt.Errorf("team %q: router selected unknown agent %q", t.ID, agentID)
	}
	result, err := a.Run(ctx, state)
	if err != nil {
		return nil, err
	}
	return graph.State(result.State), nil
}
