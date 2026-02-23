// Package agent provides the agent definition and builder API.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

	// Enhancements
	Knowledge      knowledge.Knowledge
	MemoryManager  *memory.Manager
	Hooks          hooks.Chain
	Guardrails     *guardrails.Engine
	SessionState   map[string]any // persistent cross-turn state
	OutputSchema   map[string]any // JSON Schema for structured output
	NumHistoryRuns int            // number of past runs to inject into context
	ContextCfg     ContextConfig  // context window management and summarization

	// System prompt and instructions
	SystemPrompt string
	Instructions []string

	// Multi-agent
	SubAgents              []*Agent
	MaxConcurrentSubAgents int
	Capabilities           []string // advertised capabilities for the protocol bus
}

// ContextConfig controls context window management and automatic summarization.
type ContextConfig struct {
	MaxContextTokens    int     `json:"max_context_tokens" yaml:"max_tokens"`             // override model default; 0 = use model default
	SummarizeThreshold  float64 `json:"summarize_threshold" yaml:"summarize_threshold"`    // fraction of context window to trigger summarization (default 0.8)
	PreserveRecentTurns int     `json:"preserve_recent_turns" yaml:"preserve_recent_turns"` // number of recent user/assistant pairs to keep (default 5)
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

func (b *Builder) Description(d string) *Builder              { b.agent.Description = d; return b }
func (b *Builder) WithUserID(id string) *Builder               { b.agent.UserID = id; return b }
func (b *Builder) WithModel(p model.Provider) *Builder         { b.agent.Model = p; return b }
func (b *Builder) WithStorage(s storage.Storage) *Builder      { b.agent.Storage = s; return b }
func (b *Builder) WithMemory(m *memory.Store) *Builder         { b.agent.Memory = m; return b }
func (b *Builder) WithKnowledge(k knowledge.Knowledge) *Builder { b.agent.Knowledge = k; return b }
func (b *Builder) WithMemoryManager(m *memory.Manager) *Builder { b.agent.MemoryManager = m; return b }
func (b *Builder) WithOutputSchema(s map[string]any) *Builder   { b.agent.OutputSchema = s; return b }
func (b *Builder) WithHistoryRuns(n int) *Builder               { b.agent.NumHistoryRuns = n; return b }
func (b *Builder) WithContextConfig(cfg ContextConfig) *Builder { b.agent.ContextCfg = cfg; return b }
func (b *Builder) WithSystemPrompt(prompt string) *Builder      { b.agent.SystemPrompt = prompt; return b }

func (b *Builder) AddInstruction(instruction string) *Builder {
	b.agent.Instructions = append(b.agent.Instructions, instruction)
	return b
}

func (b *Builder) AddCapability(capability string) *Builder {
	b.agent.Capabilities = append(b.agent.Capabilities, capability)
	return b
}

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

// Chat sends a single user message to the agent's model and returns the response.
// This is a convenience method for agents that have a model but no graph.
func (a *Agent) Chat(ctx context.Context, userMessage string) (*model.ChatResponse, error) {
	if a.Model == nil {
		return nil, fmt.Errorf("agent %q has no model", a.ID)
	}

	messages := make([]model.Message, 0, 8)
	if a.SystemPrompt != "" {
		messages = append(messages, model.Message{Role: model.RoleSystem, Content: a.SystemPrompt})
	}
	for _, inst := range a.Instructions {
		messages = append(messages, model.Message{Role: model.RoleSystem, Content: inst})
	}

	// Inject long-term user memories into context
	if a.MemoryManager != nil {
		if memCtx, err := a.MemoryManager.GetUserMemories(ctx); err == nil && memCtx != "" {
			messages = append(messages, model.Message{Role: model.RoleSystem, Content: memCtx})
		}
	}

	// Inject relevant knowledge via RAG
	if a.Knowledge != nil {
		if docs, err := a.Knowledge.Search(ctx, userMessage, 5); err == nil && len(docs) > 0 {
			var kb strings.Builder
			kb.WriteString("Relevant knowledge:\n")
			for _, d := range docs {
				kb.WriteString("- ")
				kb.WriteString(d.Content)
				kb.WriteString("\n")
			}
			messages = append(messages, model.Message{Role: model.RoleSystem, Content: kb.String()})
		}
	}

	messages = append(messages, model.Message{Role: model.RoleUser, Content: userMessage})

	// Check input guardrails
	if result := a.Guardrails.CheckInput(ctx, userMessage); result != nil {
		return nil, fmt.Errorf("input guardrail failed: %s", result.Reason)
	}

	req := &model.ChatRequest{
		Messages: messages,
	}

	// Apply output schema for structured output
	if a.OutputSchema != nil {
		req.ResponseFormat = "json_object"
	}

	// Add tool definitions if any are registered
	tools := a.Tools.List()
	if len(tools) > 0 {
		for _, t := range tools {
			req.Tools = append(req.Tools, model.ToolDefinition{
				Type: "function",
				Function: model.FunctionDef{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.Parameters,
				},
			})
		}
	}

	// Fire model call hooks
	modelEvt := &hooks.Event{Type: hooks.EventModelCallBefore, Name: a.Model.Name(), Input: req}
	if err := a.Hooks.Before(ctx, modelEvt); err != nil {
		return nil, fmt.Errorf("hook before model call: %w", err)
	}

	resp, err := a.Model.Chat(ctx, req)

	modelEvt.Type = hooks.EventModelCallAfter
	modelEvt.Output = resp
	modelEvt.Error = err
	_ = a.Hooks.After(ctx, modelEvt)

	if err != nil {
		return nil, fmt.Errorf("agent %q chat: %w", a.ID, err)
	}

	// Handle tool calls if the model wants to use tools
	if resp.StopReason == model.StopReasonToolCall && len(resp.ToolCalls) > 0 {
		resp, err = a.handleToolCalls(ctx, messages, resp)
		if err != nil {
			return nil, err
		}
	}

	// Check output guardrails
	if resp != nil && resp.Content != "" {
		if result := a.Guardrails.CheckOutput(ctx, resp.Content); result != nil {
			return nil, fmt.Errorf("output guardrail failed: %s", result.Reason)
		}
	}

	// Extract memories from conversation
	if a.MemoryManager != nil {
		_ = a.MemoryManager.ExtractMemories(ctx, messages)
	}

	return resp, nil
}

// handleToolCalls executes tool calls and sends results back to the model.
func (a *Agent) handleToolCalls(ctx context.Context, messages []model.Message, resp *model.ChatResponse) (*model.ChatResponse, error) {
	messages = append(messages, model.Message{
		Role:      model.RoleAssistant,
		Content:   resp.Content,
		ToolCalls: resp.ToolCalls,
	})

	for _, tc := range resp.ToolCalls {
		var args map[string]any
		_ = json.Unmarshal([]byte(tc.Arguments), &args)

		// Fire tool call hooks
		toolEvt := &hooks.Event{Type: hooks.EventToolCallBefore, Name: tc.Name, Input: args}
		if err := a.Hooks.Before(ctx, toolEvt); err != nil {
			return nil, fmt.Errorf("hook before tool %q: %w", tc.Name, err)
		}

		result, err := a.Tools.Execute(ctx, tc.Name, args)

		toolEvt.Type = hooks.EventToolCallAfter
		toolEvt.Output = result
		toolEvt.Error = err
		_ = a.Hooks.After(ctx, toolEvt)

		var content string
		if err != nil {
			content = fmt.Sprintf("Error: %s", err.Error())
		} else {
			resultJSON, _ := json.Marshal(result)
			content = string(resultJSON)
		}

		messages = append(messages, model.Message{
			Role:       model.RoleTool,
			Content:    content,
			ToolCallID: tc.ID,
			Name:       tc.Name,
		})
	}

	return a.Model.Chat(ctx, &model.ChatRequest{Messages: messages})
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

	// Post-run memory extraction
	if a.MemoryManager != nil && err == nil {
		if inputMsg, ok := input["message"].(string); ok {
			msgs := []model.Message{{Role: model.RoleUser, Content: inputMsg}}
			if result != nil {
				if resp, ok := result.State["response"].(string); ok {
					msgs = append(msgs, model.Message{Role: model.RoleAssistant, Content: resp})
				}
			}
			_ = a.MemoryManager.ExtractMemories(ctx, msgs)
		}
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
