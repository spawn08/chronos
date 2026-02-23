// Package team provides multi-agent orchestration with inter-agent communication.
//
// Teams coordinate multiple agents working together using different strategies:
// sequential, parallel, router, or coordinator (LLM-driven task decomposition).
// Agents within a team communicate via the protocol.Bus, sharing tasks and results
// the same way human developers collaborate.
package team

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/chronos-ai/chronos/engine/graph"
	"github.com/chronos-ai/chronos/sdk/agent"
	"github.com/chronos-ai/chronos/sdk/protocol"
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
	// StrategyCoordinator uses an LLM to decompose tasks and delegate to specialists.
	StrategyCoordinator Strategy = "coordinator"
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
	Order    []string // insertion order for deterministic sequential execution
	Strategy Strategy
	Router   RouterFunc
	Merge    MergeFunc
	Bus      *protocol.Bus

	// SharedContext is visible to all agents and accumulates results across the team run.
	SharedContext map[string]any
}

// New creates a team with the given strategy.
func New(id, name string, strategy Strategy) *Team {
	return &Team{
		ID:            id,
		Name:          name,
		Agents:        make(map[string]*agent.Agent),
		Strategy:      strategy,
		Bus:           protocol.NewBus(),
		SharedContext: make(map[string]any),
	}
}

// AddAgent adds an agent to the team and registers it on the communication bus.
func (t *Team) AddAgent(a *agent.Agent) *Team {
	t.Agents[a.ID] = a
	t.Order = append(t.Order, a.ID)

	_ = t.Bus.Register(a.ID, a.Name, a.Description, nil,
		func(ctx context.Context, env *protocol.Envelope) (*protocol.Envelope, error) {
			return t.handleAgentMessage(ctx, a, env)
		},
	)
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
	case StrategyCoordinator:
		return t.runCoordinator(ctx, input)
	default:
		return nil, fmt.Errorf("team %q: unknown strategy %q", t.ID, t.Strategy)
	}
}

// DelegateTask uses the bus to delegate a task from one agent to another.
func (t *Team) DelegateTask(ctx context.Context, from, to, subject string, task protocol.TaskPayload) (*protocol.ResultPayload, error) {
	return t.Bus.DelegateTask(ctx, from, to, subject, task)
}

// Broadcast sends a message from one agent to all others via the bus.
func (t *Team) Broadcast(ctx context.Context, fromAgent, subject string, data map[string]any) error {
	body, _ := json.Marshal(data)
	return t.Bus.Send(ctx, &protocol.Envelope{
		Type:    protocol.TypeBroadcast,
		From:    fromAgent,
		To:      "*",
		Subject: subject,
		Body:    body,
	})
}

// MessageHistory returns all inter-agent messages exchanged during the team run.
func (t *Team) MessageHistory() []*protocol.Envelope {
	return t.Bus.History()
}

func (t *Team) runSequential(ctx context.Context, state graph.State) (graph.State, error) {
	current := state
	for _, agentID := range t.Order {
		a := t.Agents[agentID]

		// Share accumulated context with the agent
		for k, v := range t.SharedContext {
			if _, exists := current[k]; !exists {
				current[k] = v
			}
		}

		result, err := a.Run(ctx, current)
		if err != nil {
			return nil, fmt.Errorf("team %q: agent %q: %w", t.ID, a.ID, err)
		}
		current = graph.State(result.State)

		// Accumulate results into shared context
		for k, v := range current {
			t.SharedContext[k] = v
		}

		// Broadcast result to other agents for awareness
		t.Broadcast(ctx, a.ID, fmt.Sprintf("completed:%s", a.Name), current)
	}
	return current, nil
}

func (t *Team) runParallel(ctx context.Context, input graph.State) (graph.State, error) {
	var mu sync.Mutex
	var results []graph.State
	var firstErr error

	var wg sync.WaitGroup
	for _, agentID := range t.Order {
		a := t.Agents[agentID]
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

// runCoordinator uses an LLM-like decomposition: the first agent acts as coordinator,
// breaking tasks into sub-tasks and delegating to specialists via the bus.
func (t *Team) runCoordinator(ctx context.Context, input graph.State) (graph.State, error) {
	if len(t.Order) < 2 {
		return nil, fmt.Errorf("team %q: coordinator strategy requires at least 2 agents", t.ID)
	}

	coordinatorID := t.Order[0]
	coordinator := t.Agents[coordinatorID]

	// The coordinator runs first to produce a plan
	result, err := coordinator.Run(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("team %q: coordinator %q: %w", t.ID, coordinatorID, err)
	}

	state := graph.State(result.State)

	// Delegate to each specialist sequentially via the bus
	for _, agentID := range t.Order[1:] {
		taskPayload := protocol.TaskPayload{
			Description: fmt.Sprintf("Execute sub-task from coordinator for %s", agentID),
			Input:       state,
		}

		taskResult, err := t.Bus.DelegateTask(ctx, coordinatorID, agentID, "subtask", taskPayload)
		if err != nil {
			return nil, fmt.Errorf("team %q: delegate to %q: %w", t.ID, agentID, err)
		}

		if taskResult.Success {
			for k, v := range taskResult.Output {
				state[k] = v
			}
		} else if taskResult.Error != "" {
			return nil, fmt.Errorf("team %q: agent %q failed: %s", t.ID, agentID, taskResult.Error)
		}
	}

	return state, nil
}

// handleAgentMessage is called when an agent receives a message on the bus.
func (t *Team) handleAgentMessage(ctx context.Context, a *agent.Agent, env *protocol.Envelope) (*protocol.Envelope, error) {
	switch env.Type {
	case protocol.TypeTaskRequest:
		var task protocol.TaskPayload
		if err := json.Unmarshal(env.Body, &task); err != nil {
			return nil, fmt.Errorf("decode task: %w", err)
		}

		input := task.Input
		if input == nil {
			input = make(map[string]any)
		}
		input["_task_description"] = task.Description
		input["_delegated_by"] = env.From

		result, err := a.Run(ctx, input)

		var resultPayload protocol.ResultPayload
		resultPayload.TaskID = env.ID
		if err != nil {
			resultPayload.Success = false
			resultPayload.Error = err.Error()
		} else {
			resultPayload.Success = true
			resultPayload.Output = result.State
		}

		body, _ := json.Marshal(resultPayload)
		return &protocol.Envelope{
			Type:    protocol.TypeTaskResult,
			Subject: "task_result",
			Body:    body,
		}, nil

	case protocol.TypeQuestion:
		var q map[string]string
		_ = json.Unmarshal(env.Body, &q)

		input := map[string]any{
			"_question":   q["question"],
			"_asked_by":   env.From,
			"message":     q["question"],
		}
		result, err := a.Run(ctx, input)
		if err != nil {
			return nil, err
		}

		answer := result.State["response"]
		if answer == nil {
			answer = result.State["answer"]
		}
		body, _ := json.Marshal(map[string]any{"answer": answer})
		return &protocol.Envelope{
			Type:    protocol.TypeAnswer,
			Subject: "answer",
			Body:    body,
		}, nil

	case protocol.TypeBroadcast:
		// Store broadcast data in shared context
		var data map[string]any
		if err := json.Unmarshal(env.Body, &data); err == nil {
			for k, v := range data {
				t.SharedContext[k] = v
			}
		}
		return nil, nil

	default:
		return nil, nil
	}
}
