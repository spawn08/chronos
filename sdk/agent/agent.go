// Package agent provides the agent definition and builder API.
package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/chronos-ai/chronos/engine/graph"
	"github.com/chronos-ai/chronos/engine/guardrails"
	"github.com/chronos-ai/chronos/engine/hooks"
	"github.com/chronos-ai/chronos/engine/model"
	"github.com/chronos-ai/chronos/engine/tool"
	"github.com/chronos-ai/chronos/sdk/knowledge"
	"github.com/chronos-ai/chronos/sdk/memory"
	"github.com/chronos-ai/chronos/sdk/skill"
	"github.com/chronos-ai/chronos/storage"
)

// Agent is the top-level agent definition.
type Agent struct {
	ID          string
	Name        string
	Description string
	UserID      string

	// Core components
	Model   model.Provider
	Tools   *tool.Registry
	Skills  *skill.Registry
	Memory  *memory.Store
	Storage storage.Storage
	Graph   *graph.CompiledGraph

	// Agno-inspired enhancements
	Knowledge      knowledge.Knowledge
	MemoryManager  *memory.Manager
	Hooks          hooks.Chain
	Guardrails     *guardrails.Engine
	SessionState   map[string]any // persistent cross-turn state
	OutputSchema   map[string]any // JSON Schema for structured output
	NumHistoryRuns int            // number of past runs to inject into context

	// Multi-agent
	SubAgents              []*Agent
	MaxConcurrentSubAgents int
}

// Builder provides a fluent API for constructing agents.
type Builder struct {
	agent *Agent
	graph *graph.StateGraph
}

// New creates a new agent builder.
func New(id, name string) *Builder {
	return &Builder{
		agent: &Agent{
			ID:                     id,
			Name:                   name,
			Tools:                  tool.NewRegistry(),
			Skills:                 skill.NewRegistry(),
			Guardrails:             guardrails.NewEngine(),
			SessionState:           make(map[string]any),
			MaxConcurrentSubAgents: 5,
		},
	}
}

func (b *Builder) Description(d string) *Builder            { b.agent.Description = d; return b }
func (b *Builder) WithUserID(id string) *Builder             { b.agent.UserID = id; return b }
func (b *Builder) WithModel(p model.Provider) *Builder       { b.agent.Model = p; return b }
func (b *Builder) WithStorage(s storage.Storage) *Builder    { b.agent.Storage = s; return b }
func (b *Builder) WithMemory(m *memory.Store) *Builder       { b.agent.Memory = m; return b }
func (b *Builder) WithKnowledge(k knowledge.Knowledge) *Builder { b.agent.Knowledge = k; return b }
func (b *Builder) WithMemoryManager(m *memory.Manager) *Builder { b.agent.MemoryManager = m; return b }
func (b *Builder) WithOutputSchema(s map[string]any) *Builder { b.agent.OutputSchema = s; return b }
func (b *Builder) WithHistoryRuns(n int) *Builder             { b.agent.NumHistoryRuns = n; return b }

func (b *Builder) AddTool(def *tool.Definition) *Builder {
	b.agent.Tools.Register(def)
	return b
}

func (b *Builder) AddSkill(s *skill.Skill) *Builder {
	b.agent.Skills.Register(s)
	return b
}

func (b *Builder) AddSubAgent(sub *Agent) *Builder {
	b.agent.SubAgents = append(b.agent.SubAgents, sub)
	return b
}

func (b *Builder) AddHook(h hooks.Hook) *Builder {
	b.agent.Hooks = append(b.agent.Hooks, h)
	return b
}

func (b *Builder) AddInputGuardrail(name string, g guardrails.Guardrail) *Builder {
	b.agent.Guardrails.AddRule(guardrails.Rule{Name: name, Position: guardrails.Input, Guardrail: g})
	return b
}

func (b *Builder) AddOutputGuardrail(name string, g guardrails.Guardrail) *Builder {
	b.agent.Guardrails.AddRule(guardrails.Rule{Name: name, Position: guardrails.Output, Guardrail: g})
	return b
}

func (b *Builder) WithGraph(g *graph.StateGraph) *Builder {
	b.graph = g
	return b
}

// Build compiles the graph (if set) and returns the agent.
func (b *Builder) Build() (*Agent, error) {
	if b.graph != nil {
		compiled, err := b.graph.Compile()
		if err != nil {
			return nil, fmt.Errorf("agent %q: %w", b.agent.ID, err)
		}
		b.agent.Graph = compiled
	}
	return b.agent, nil
}

// Run starts a new execution session for this agent.
func (a *Agent) Run(ctx context.Context, input map[string]any) (*graph.RunState, error) {
	if a.Graph == nil {
		return nil, fmt.Errorf("agent %q has no graph", a.ID)
	}
	if a.Storage == nil {
		return nil, fmt.Errorf("agent %q has no storage", a.ID)
	}

	// Check input guardrails
	if inputMsg, ok := input["message"].(string); ok {
		if result := a.Guardrails.CheckInput(ctx, inputMsg); result != nil {
			return nil, fmt.Errorf("input guardrail failed: %s", result.Reason)
		}
	}

	sess := &storage.Session{
		ID:        fmt.Sprintf("sess_%d", time.Now().UnixNano()),
		AgentID:   a.ID,
		Status:    "running",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := a.Storage.CreateSession(ctx, sess); err != nil {
		return nil, err
	}

	// Fire before hooks
	evt := &hooks.Event{Type: hooks.EventNodeBefore, Name: "run_start", Input: input}
	if err := a.Hooks.Before(ctx, evt); err != nil {
		return nil, fmt.Errorf("hook before run: %w", err)
	}

	runner := graph.NewRunner(a.Graph, a.Storage)
	result, err := runner.Run(ctx, sess.ID, graph.State(input))

	// Fire after hooks
	evt.Type = hooks.EventNodeAfter
	evt.Output = result
	if hookErr := a.Hooks.After(ctx, evt); hookErr != nil && err == nil {
		err = hookErr
	}

	return result, err
}

// Resume continues a paused session.
func (a *Agent) Resume(ctx context.Context, sessionID string) (*graph.RunState, error) {
	if a.Graph == nil || a.Storage == nil {
		return nil, fmt.Errorf("agent %q: graph or storage not set", a.ID)
	}
	runner := graph.NewRunner(a.Graph, a.Storage)
	return runner.Resume(ctx, sessionID)
}
