---
title: "Agent Builder API"
permalink: /api/agent-builder/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
---

The agent builder provides a fluent API for constructing fully-wired agents.

## Constructor

```go
agent.New(id string, name string) *Builder
```

Creates a new builder with defaults: empty tool registry, empty skill registry, guardrails engine, and `MaxConcurrentSubAgents` set to 5.

## Builder Methods

All builder methods return `*Builder` for chaining.

### Identity

| Method | Description |
|--------|-------------|
| `Description(d string)` | Set agent description |
| `WithUserID(id string)` | Set the user ID for memory scoping |

### Core Components

| Method | Description |
|--------|-------------|
| `WithModel(p model.Provider)` | Set the LLM provider |
| `WithStorage(s storage.Storage)` | Set the persistence backend |
| `WithMemory(m *memory.Store)` | Set the memory store |
| `WithKnowledge(k knowledge.Knowledge)` | Set the RAG knowledge base |
| `WithMemoryManager(m *memory.Manager)` | Set the LLM-powered memory manager |
| `WithGraph(g *graph.StateGraph)` | Set the execution graph |

### Configuration

| Method | Description |
|--------|-------------|
| `WithSystemPrompt(prompt string)` | Set the system prompt |
| `AddInstruction(instruction string)` | Append an instruction to system context |
| `WithOutputSchema(s map[string]any)` | Set JSON Schema for structured output |
| `WithHistoryRuns(n int)` | Number of past runs to inject into context |
| `WithContextConfig(cfg ContextConfig)` | Configure context window management |

### ContextConfig

```go
type ContextConfig struct {
    MaxContextTokens    int     // override model default; 0 = use model default
    SummarizeThreshold  float64 // fraction of context to trigger summarization (default 0.8)
    PreserveRecentTurns int     // recent user/assistant pairs to keep (default 5)
}
```

### Tools and Skills

| Method | Description |
|--------|-------------|
| `AddTool(def *tool.Definition)` | Register a tool the model can call |
| `AddSkill(s *skill.Skill)` | Register a skill |
| `AddCapability(capability string)` | Advertise a capability for the protocol bus |

### Middleware

| Method | Description |
|--------|-------------|
| `AddHook(h hooks.Hook)` | Append a middleware hook |
| `AddInputGuardrail(name string, g guardrails.Guardrail)` | Add input validation |
| `AddOutputGuardrail(name string, g guardrails.Guardrail)` | Add output validation |

### Multi-Agent

| Method | Description |
|--------|-------------|
| `AddSubAgent(sub *Agent)` | Register a sub-agent |

### Build

```go
func (b *Builder) Build() (*Agent, error)
```

Compiles the graph (if set) and returns the configured agent. Returns an error if graph compilation fails.

## Agent Methods

### Chat (Single-Turn)

```go
func (a *Agent) Chat(ctx context.Context, userMessage string) (*model.ChatResponse, error)
```

Sends a single user message to the model. Stateless -- no conversation history is maintained between calls.

**Flow:**
1. Build messages: system prompt, instructions, memories, knowledge, user message
2. Check input guardrails
3. Fire `model_call.before` hooks
4. Call `model.Provider.Chat`
5. Fire `model_call.after` hooks
6. Handle tool calls (if any)
7. Check output guardrails
8. Extract memories

### ChatWithSession (Multi-Turn)

```go
func (a *Agent) ChatWithSession(ctx context.Context, sessionID, userMessage string) (*model.ChatResponse, error)
```

Sends a message within a persistent, multi-turn session. Messages are stored in the event ledger. When the conversation approaches the model's context window limit, older messages are automatically summarized.

**Requires:** `Storage` must be set on the agent.

**Flow:**
1. Load or create session
2. Reconstruct conversation from events
3. Append user message
4. Check if summarization is needed
5. Summarize older messages (if threshold exceeded)
6. Build messages with system context + summary + recent history
7. Call model, handle tool calls, check guardrails
8. Persist assistant response

### Run (Graph Execution)

```go
func (a *Agent) Run(ctx context.Context, input map[string]any) (*graph.RunState, error)
```

Starts a new execution session using the agent's StateGraph. State is checkpointed after every node.

**Requires:** `Graph` and `Storage` must be set.

### Resume

```go
func (a *Agent) Resume(ctx context.Context, sessionID string) (*graph.RunState, error)
```

Continues a paused session from the latest checkpoint.

## YAML Configuration

Agents can also be built from YAML config:

```go
fc, _ := agent.LoadFile("")
cfg, _ := fc.FindAgent("dev")
a, _ := agent.BuildAgent(ctx, cfg)
```

See the [Configuration guide]({{ '/getting-started/configuration/' | relative_url }}) for full YAML reference.

## Complete Example

```go
store, _ := sqlite.New("app.db")
store.Migrate(ctx)

tracker := hooks.NewCostTracker(nil)

a, _ := agent.New("assistant", "AI Assistant").
    WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
    WithStorage(store).
    WithSystemPrompt("You are a helpful coding assistant.").
    AddInstruction("Always include code examples.").
    WithContextConfig(agent.ContextConfig{
        SummarizeThreshold:  0.8,
        PreserveRecentTurns: 5,
    }).
    AddTool(&tool.Definition{
        Name:        "search_docs",
        Description: "Search project documentation",
        Parameters:  map[string]any{"type": "object", "properties": map[string]any{
            "query": map[string]string{"type": "string"},
        }},
        Permission: tool.PermAllow,
        Handler:    searchHandler,
    }).
    AddHook(hooks.NewRetryHook(3)).
    AddHook(tracker).
    AddInputGuardrail("blocklist", &guardrails.BlocklistGuardrail{
        Blocklist: []string{"password", "secret"},
    }).
    Build()

// Multi-turn session with automatic summarization
resp, _ := a.ChatWithSession(ctx, "session-001", "How do I implement auth?")
fmt.Println(resp.Content)
fmt.Printf("Cost: $%.6f\n", tracker.GetGlobalCost().TotalCost)
```
