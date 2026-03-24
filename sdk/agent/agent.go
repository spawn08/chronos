// Package agent provides the agent definition and builder API.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/guardrails"
	"github.com/spawn08/chronos/engine/hooks"
	"github.com/spawn08/chronos/engine/mcp"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/stream"
	"github.com/spawn08/chronos/engine/tool"
	chronostrace "github.com/spawn08/chronos/os/trace"
	"github.com/spawn08/chronos/sdk/knowledge"
	"github.com/spawn08/chronos/sdk/memory"
	"github.com/spawn08/chronos/sdk/skill"
	"github.com/spawn08/chronos/storage"
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
	Broker         *stream.Broker          // SSE broker for real-time event streaming
	Tracer         *chronostrace.Collector // execution tracer for span-based observability
	SessionState   map[string]any          // persistent cross-turn state
	OutputSchema   map[string]any          // JSON Schema for structured output
	NumHistoryRuns int                     // number of past runs to inject into context
	ContextCfg     ContextConfig           // context window management and summarization

	// System prompt and instructions
	SystemPrompt string
	Instructions []string

	// MCP servers
	MCPClients []*mcp.Client

	// Multi-agent
	SubAgents              []*Agent
	MaxConcurrentSubAgents int
	Capabilities           []string // advertised capabilities for the protocol bus
}

// ContextConfig controls context window management and automatic summarization.
type ContextConfig struct {
	MaxContextTokens        int     `json:"max_context_tokens" yaml:"max_tokens"`                 // override model default; 0 = use model default
	SummarizeThreshold      float64 `json:"summarize_threshold" yaml:"summarize_threshold"`       // fraction of context window to trigger summarization (default 0.8)
	PreserveRecentTurns     int     `json:"preserve_recent_turns" yaml:"preserve_recent_turns"`   // number of recent user/assistant pairs to keep (default 5)
	MaxToolResultTokens     int     `json:"max_tool_result_tokens" yaml:"max_tool_result_tokens"` // max tokens for tool result before eviction (default 20000)
	MaxToolCallsFromHistory int     `json:"max_tool_calls_from_history" yaml:"max_tool_calls"`    // max tool call pairs to keep in history
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

func (b *Builder) Description(d string) *Builder                 { b.agent.Description = d; return b }
func (b *Builder) WithUserID(id string) *Builder                 { b.agent.UserID = id; return b }
func (b *Builder) WithModel(p model.Provider) *Builder           { b.agent.Model = p; return b }
func (b *Builder) WithStorage(s storage.Storage) *Builder        { b.agent.Storage = s; return b }
func (b *Builder) WithMemory(m *memory.Store) *Builder           { b.agent.Memory = m; return b }
func (b *Builder) WithKnowledge(k knowledge.Knowledge) *Builder  { b.agent.Knowledge = k; return b }
func (b *Builder) WithMemoryManager(m *memory.Manager) *Builder  { b.agent.MemoryManager = m; return b }
func (b *Builder) WithOutputSchema(s map[string]any) *Builder    { b.agent.OutputSchema = s; return b }
func (b *Builder) WithHistoryRuns(n int) *Builder                { b.agent.NumHistoryRuns = n; return b }
func (b *Builder) WithContextConfig(cfg ContextConfig) *Builder  { b.agent.ContextCfg = cfg; return b }
func (b *Builder) WithBroker(br *stream.Broker) *Builder         { b.agent.Broker = br; return b }
func (b *Builder) WithTracer(t *chronostrace.Collector) *Builder { b.agent.Tracer = t; return b }
func (b *Builder) WithSystemPrompt(prompt string) *Builder       { b.agent.SystemPrompt = prompt; return b }

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

func (b *Builder) AddMCPServer(cfg mcp.ServerConfig) *Builder {
	client, err := mcp.NewClient(cfg)
	if err == nil {
		b.agent.MCPClients = append(b.agent.MCPClients, client)
	}
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

// ConnectMCP connects all configured MCP servers and registers their tools
// in the agent's tool registry. Call after Build.
func (a *Agent) ConnectMCP(ctx context.Context) error {
	for _, client := range a.MCPClients {
		if err := client.Connect(ctx); err != nil {
			return fmt.Errorf("mcp connect %q: %w", client.Info().Name, err)
		}
		if _, err := mcp.RegisterTools(ctx, client, a.Tools); err != nil {
			return fmt.Errorf("mcp register tools: %w", err)
		}
	}
	return nil
}

// CloseMCP disconnects all MCP server connections.
func (a *Agent) CloseMCP() {
	for _, client := range a.MCPClients {
		client.Close()
	}
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

	// P0-004: Inject past run history from storage
	if a.NumHistoryRuns > 0 && a.Storage != nil {
		historyMsgs := a.loadHistoryRuns(ctx)
		if len(historyMsgs) > 0 {
			messages = append(messages, model.Message{
				Role:    model.RoleSystem,
				Content: "Previous conversation history:\n" + formatHistoryMessages(historyMsgs),
			})
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

	// P0-005: Apply output schema — pass the full JSON Schema, not just json_object mode
	applyOutputSchema(req, a.OutputSchema)

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

	// Fire model call hooks, passing provider and request for retry hook
	modelEvt := &hooks.Event{
		Type:  hooks.EventModelCallBefore,
		Name:  a.Model.Name(),
		Input: req,
		Metadata: map[string]any{
			"provider": a.Model,
			"request":  req,
		},
	}
	if err := a.Hooks.Before(ctx, modelEvt); err != nil {
		return nil, fmt.Errorf("hook before model call: %w", err)
	}

	if a.Broker != nil {
		a.Broker.Publish(stream.Event{Type: stream.EventModelCall, Data: map[string]any{
			"agent": a.ID, "model": a.Model.Name(), "messages": len(req.Messages),
		}})
	}

	var modelSpan *storage.Trace
	if a.Tracer != nil {
		var spanErr error
		modelSpan, spanErr = a.Tracer.StartSpan(ctx, a.ID, "model:"+a.Model.Name(), "model_call")
		if spanErr != nil {
			modelSpan = nil
		}
	}

	resp, err := a.Model.Chat(ctx, req)

	modelEvt.Type = hooks.EventModelCallAfter
	modelEvt.Output = resp
	modelEvt.Error = err
	_ = a.Hooks.After(ctx, modelEvt)

	// If retry hook succeeded, use its output
	if err != nil && modelEvt.Error == nil {
		resp, _ = modelEvt.Output.(*model.ChatResponse)
		err = nil
	}

	if err != nil {
		if modelSpan != nil {
			_ = a.Tracer.EndSpan(ctx, modelSpan, nil, err.Error())
		}
		if a.Broker != nil {
			a.Broker.Publish(stream.Event{Type: stream.EventError, Data: map[string]any{
				"agent": a.ID, "error": err.Error(),
			}})
		}
		return nil, fmt.Errorf("agent %q chat: %w", a.ID, err)
	}

	if modelSpan != nil {
		_ = a.Tracer.EndSpan(ctx, modelSpan, map[string]any{"stop_reason": string(resp.StopReason)}, "")
	}

	if a.Broker != nil {
		a.Broker.Publish(stream.Event{Type: stream.EventModelResponse, Data: map[string]any{
			"agent": a.ID, "stop_reason": string(resp.StopReason),
		}})
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

	// P0-005: Validate response against output schema
	if a.OutputSchema != nil && resp != nil && resp.Content != "" {
		if valErr := validateAgainstSchema(resp.Content, a.OutputSchema); valErr != nil {
			return nil, fmt.Errorf("output schema validation failed: %w", valErr)
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

		if a.Broker != nil {
			a.Broker.Publish(stream.Event{Type: stream.EventToolCall, Data: map[string]any{
				"agent": a.ID, "tool": tc.Name, "args": args,
			}})
		}

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

		if a.Broker != nil {
			toolResultData := map[string]any{"agent": a.ID, "tool": tc.Name}
			if err != nil {
				toolResultData["error"] = err.Error()
			} else {
				toolResultData["result"] = result
			}
			a.Broker.Publish(stream.Event{Type: stream.EventToolResult, Data: toolResultData})
		}

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

// Execute runs the agent on a text task and returns the text response.
// Unlike Run, Execute works with just a model — no graph or storage needed.
// This is the primary entry point for team-based orchestration where agents
// are lightweight task executors.
func (a *Agent) Execute(ctx context.Context, task string) (string, error) {
	if a.Model == nil {
		return "", fmt.Errorf("agent %q: no model configured", a.ID)
	}

	resp, err := a.Chat(ctx, task)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

// Run starts a new execution session for this agent.
func (a *Agent) Run(ctx context.Context, input map[string]any) (*graph.RunState, error) {
	// If no graph, use model-only execution via Execute
	if a.Graph == nil && a.Model != nil {
		msg, _ := input["message"].(string)
		if msg == "" {
			msg = stateToPrompt(input)
		}
		content, err := a.Execute(ctx, msg)
		if err != nil {
			return nil, err
		}
		out := make(map[string]any, len(input)+1)
		for k, v := range input {
			out[k] = v
		}
		out["response"] = content
		return &graph.RunState{
			Status: graph.RunStatusCompleted,
			State:  graph.State(out),
		}, nil
	}

	if a.Graph == nil {
		return nil, fmt.Errorf("agent %q has no graph or model", a.ID)
	}
	if a.Storage == nil {
		return nil, fmt.Errorf("agent %q has no storage", a.ID)
	}

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

	evt := &hooks.Event{Type: hooks.EventNodeBefore, Name: "run_start", Input: input}
	if err := a.Hooks.Before(ctx, evt); err != nil {
		return nil, fmt.Errorf("hook before run: %w", err)
	}

	runner := graph.NewRunner(a.Graph, a.Storage)
	if a.Broker != nil {
		runner.WithBroker(a.Broker)
	}
	if a.Tracer != nil {
		runner.WithTracer(a.Tracer)
	}
	result, err := runner.Run(ctx, sess.ID, graph.State(input))

	evt.Type = hooks.EventNodeAfter
	evt.Output = result
	if hookErr := a.Hooks.After(ctx, evt); hookErr != nil && err == nil {
		err = hookErr
	}

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

// stateToPrompt converts a state map into a textual prompt for model-only execution.
func stateToPrompt(state map[string]any) string {
	var b strings.Builder
	for k, v := range state {
		if strings.HasPrefix(k, "_") {
			continue
		}
		fmt.Fprintf(&b, "%s: %v\n", k, v)
	}
	return b.String()
}

// Resume continues a paused session.
func (a *Agent) Resume(ctx context.Context, sessionID string) (*graph.RunState, error) {
	if a.Graph == nil || a.Storage == nil {
		return nil, fmt.Errorf("agent %q: graph or storage not set", a.ID)
	}
	runner := graph.NewRunner(a.Graph, a.Storage)
	if a.Broker != nil {
		runner.WithBroker(a.Broker)
	}
	if a.Tracer != nil {
		runner.WithTracer(a.Tracer)
	}
	return runner.Resume(ctx, sessionID)
}

// loadHistoryRuns retrieves past conversation messages from storage for context injection.
// It loads events from the most recent sessions up to NumHistoryRuns.
func (a *Agent) loadHistoryRuns(ctx context.Context) []model.Message {
	if a.Storage == nil || a.NumHistoryRuns <= 0 {
		return nil
	}

	sessions, err := a.Storage.ListSessions(ctx, a.ID, a.NumHistoryRuns, 0)
	if err != nil || len(sessions) == 0 {
		return nil
	}

	var history []model.Message
	for _, sess := range sessions {
		events, err := a.Storage.ListEvents(ctx, sess.ID, 0)
		if err != nil {
			continue
		}
		cs := chatSessionFromEvents(events)
		for _, msg := range cs.Messages {
			if msg.Role == model.RoleUser || msg.Role == model.RoleAssistant {
				history = append(history, msg)
			}
		}
	}
	return history
}

// formatHistoryMessages converts a slice of messages into a readable string.
func formatHistoryMessages(msgs []model.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case model.RoleUser:
			b.WriteString("User: ")
		case model.RoleAssistant:
			b.WriteString("Assistant: ")
		default:
			b.WriteString(m.Role + ": ")
		}
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// applyOutputSchema sets the request's response format using the full JSON Schema
// rather than just requesting json_object mode.
func applyOutputSchema(req *model.ChatRequest, schema map[string]any) {
	if schema == nil {
		return
	}
	req.ResponseFormat = "json_schema"
	if req.Metadata == nil {
		req.Metadata = make(map[string]any)
	}
	req.Metadata["json_schema"] = schema
}

// validateAgainstSchema performs basic structural validation of a JSON response
// against the provided JSON Schema. It checks required fields and basic types.
func validateAgainstSchema(content string, schema map[string]any) error {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return fmt.Errorf("response is not valid JSON: %w", err)
	}

	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		return nil
	}

	required, _ := schema["required"].([]any)
	for _, r := range required {
		fieldName, ok := r.(string)
		if !ok {
			continue
		}
		if _, exists := parsed[fieldName]; !exists {
			return fmt.Errorf("required field %q is missing from response", fieldName)
		}
	}

	for fieldName, fieldSpec := range props {
		val, exists := parsed[fieldName]
		if !exists {
			continue
		}
		specMap, ok := fieldSpec.(map[string]any)
		if !ok {
			continue
		}
		expectedType, _ := specMap["type"].(string)
		if expectedType == "" {
			continue
		}
		if typeErr := checkJSONType(fieldName, val, expectedType); typeErr != nil {
			return typeErr
		}
	}

	return nil
}

// checkJSONType validates that a JSON value matches the expected JSON Schema type.
func checkJSONType(field string, val any, expected string) error {
	switch expected {
	case "string":
		if _, ok := val.(string); !ok {
			return fmt.Errorf("field %q: expected string, got %T", field, val)
		}
	case "number", "integer":
		if _, ok := val.(float64); !ok {
			return fmt.Errorf("field %q: expected number, got %T", field, val)
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("field %q: expected boolean, got %T", field, val)
		}
	case "array":
		if _, ok := val.([]any); !ok {
			return fmt.Errorf("field %q: expected array, got %T", field, val)
		}
	case "object":
		if _, ok := val.(map[string]any); !ok {
			return fmt.Errorf("field %q: expected object, got %T", field, val)
		}
	}
	return nil
}
