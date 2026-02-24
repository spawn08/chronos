// Package team provides multi-agent orchestration with inter-agent communication.
//
// Teams coordinate multiple agents working together using different strategies:
// sequential (pipeline), parallel (fan-out/fan-in), router (intelligent dispatch),
// or coordinator (LLM-driven task decomposition). Communication flows through the
// protocol.Bus with optional direct agent-to-agent channels for low-latency paths.
package team

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/protocol"
)

// Strategy defines how work is distributed across team members.
type Strategy string

const (
	StrategySequential  Strategy = "sequential"
	StrategyParallel    Strategy = "parallel"
	StrategyRouter      Strategy = "router"
	StrategyCoordinator Strategy = "coordinator"
)

// RouterFunc selects an agent ID based on the current state.
type RouterFunc func(state graph.State) string

// ModelRouterFunc selects an agent using model-based reasoning. It receives the
// full state plus a list of available agents with descriptions.
type ModelRouterFunc func(ctx context.Context, state graph.State, agents []AgentInfo) (string, error)

// MergeFunc combines results from parallel agent executions.
type MergeFunc func(results []graph.State) graph.State

// ErrorStrategy controls how agent failures are handled.
type ErrorStrategy int

const (
	ErrorStrategyFailFast  ErrorStrategy = iota // abort on first error
	ErrorStrategyCollect                        // collect all errors, return combined
	ErrorStrategyBestEffort                     // ignore errors, return successful results
)

// AgentInfo is a lightweight descriptor exposed to routing functions.
type AgentInfo struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Capabilities []string `json:"capabilities"`
}

// Team orchestrates multiple agents working together.
type Team struct {
	ID       string
	Name     string
	Agents   map[string]*agent.Agent
	Order    []string
	Strategy Strategy

	Router      RouterFunc
	ModelRouter ModelRouterFunc
	Merge       MergeFunc
	Bus         *protocol.Bus

	MaxConcurrency int           // for parallel strategy; 0 = unbounded
	ErrorMode      ErrorStrategy // how to handle agent failures
	Coordinator    *agent.Agent  // explicit coordinator agent (for StrategyCoordinator)
	MaxIterations  int           // max coordinator planning iterations; 0 = 1

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
		MaxIterations: 1,
	}
}

// AddAgent adds an agent to the team and registers it on the communication bus.
func (t *Team) AddAgent(a *agent.Agent) *Team {
	t.Agents[a.ID] = a
	t.Order = append(t.Order, a.ID)

	_ = t.Bus.Register(a.ID, a.Name, a.Description, a.Capabilities,
		func(ctx context.Context, env *protocol.Envelope) (*protocol.Envelope, error) {
			return t.handleAgentMessage(ctx, a, env)
		},
	)
	return t
}

// SetRouter sets a static routing function (for StrategyRouter).
func (t *Team) SetRouter(fn RouterFunc) *Team {
	t.Router = fn
	return t
}

// SetModelRouter sets a model-based routing function (for StrategyRouter).
func (t *Team) SetModelRouter(fn ModelRouterFunc) *Team {
	t.ModelRouter = fn
	return t
}

// SetMerge sets the merge function (for StrategyParallel).
func (t *Team) SetMerge(fn MergeFunc) *Team {
	t.Merge = fn
	return t
}

// SetMaxConcurrency limits goroutines for parallel execution.
func (t *Team) SetMaxConcurrency(n int) *Team {
	t.MaxConcurrency = n
	return t
}

// SetErrorStrategy controls how agent failures are handled.
func (t *Team) SetErrorStrategy(es ErrorStrategy) *Team {
	t.ErrorMode = es
	return t
}

// SetCoordinator sets an explicit coordinator agent (for StrategyCoordinator).
// The coordinator is automatically registered on the bus so it can delegate tasks.
func (t *Team) SetCoordinator(a *agent.Agent) *Team {
	t.Coordinator = a
	_ = t.Bus.Register(a.ID, a.Name, a.Description, a.Capabilities,
		func(ctx context.Context, env *protocol.Envelope) (*protocol.Envelope, error) {
			return t.handleAgentMessage(ctx, a, env)
		},
	)
	return t
}

// SetMaxIterations sets the max number of coordinator planning iterations.
func (t *Team) SetMaxIterations(n int) *Team {
	t.MaxIterations = n
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

// DirectChannel returns (or creates) a dedicated low-latency channel between
// two agents that bypasses the central bus.
func (t *Team) DirectChannel(agentA, agentB string, bufSize int) *protocol.DirectChannel {
	return t.Bus.DirectChannelBetween(agentA, agentB, bufSize)
}

// MessageHistory returns all inter-agent messages exchanged during the team run.
func (t *Team) MessageHistory() []*protocol.Envelope {
	return t.Bus.History()
}

// agentInfoList returns lightweight descriptors for all agents.
func (t *Team) agentInfoList() []AgentInfo {
	infos := make([]AgentInfo, 0, len(t.Order))
	for _, id := range t.Order {
		a := t.Agents[id]
		infos = append(infos, AgentInfo{
			ID:           a.ID,
			Name:         a.Name,
			Description:  a.Description,
			Capabilities: a.Capabilities,
		})
	}
	return infos
}

// executeAgent runs an agent on a state, using Execute (model-only) when possible,
// falling back to Run (graph-based).
func executeAgent(ctx context.Context, a *agent.Agent, state graph.State) (graph.State, error) {
	msg, _ := state["message"].(string)
	if msg == "" {
		msg = stateToPrompt(state)
	}

	content, err := a.Execute(ctx, msg)
	if err != nil {
		result, runErr := a.Run(ctx, state)
		if runErr != nil {
			return nil, runErr
		}
		return result.State, nil
	}

	out := make(graph.State, len(state)+1)
	for k, v := range state {
		out[k] = v
	}
	out["response"] = content
	return out, nil
}

// stateToPrompt converts a state map into a textual prompt.
func stateToPrompt(state graph.State) string {
	var result string
	for k, v := range state {
		if k == "_task_description" || k == "_delegated_by" {
			continue
		}
		result += fmt.Sprintf("%s: %v\n", k, v)
	}
	return result
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

		state, err := executeAgent(ctx, a, input)

		var resultPayload protocol.ResultPayload
		resultPayload.TaskID = env.ID
		if err != nil {
			resultPayload.Success = false
			resultPayload.Error = err.Error()
		} else {
			resultPayload.Success = true
			resultPayload.Output = state
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

		input := graph.State{
			"_question": q["question"],
			"_asked_by": env.From,
			"message":   q["question"],
		}
		state, err := executeAgent(ctx, a, input)
		if err != nil {
			return nil, err
		}

		answer := state["response"]
		if answer == nil {
			answer = state["answer"]
		}
		body, _ := json.Marshal(map[string]any{"answer": answer})
		return &protocol.Envelope{
			Type:    protocol.TypeAnswer,
			Subject: "answer",
			Body:    body,
		}, nil

	case protocol.TypeBroadcast:
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
