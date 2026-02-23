---
title: "Agents"
permalink: /guides/agents/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
---

The Chronos agent is the central abstraction for building AI-powered applications. Agents combine a language model, tools, memory, knowledge, and optional graph-based workflows into a single, configurable unit.

## Agent Struct

The `Agent` struct holds all configuration and runtime components:

| Field | Type | Description |
|-------|------|--------------|
| `ID` | string | Unique identifier for the agent |
| `Name` | string | Human-readable display name |
| `Description` | string | Optional description of the agent's purpose |
| `UserID` | string | Optional user scope for multi-tenant scenarios |
| `Model` | model.Provider | LLM backend for chat completions |
| `Tools` | *tool.Registry | Registered tools for function calling |
| `Skills` | *skill.Registry | Reusable skill definitions |
| `Memory` | *memory.Store | Short-term and long-term memory store |
| `Storage` | storage.Storage | Persistence for sessions, events, checkpoints |
| `Graph` | *graph.CompiledGraph | Durable workflow graph (optional) |
| `Knowledge` | knowledge.Knowledge | RAG knowledge base (optional) |
| `MemoryManager` | *memory.Manager | LLM-powered memory extraction (optional) |
| `Hooks` | hooks.Chain | Before/after middleware for execution events |
| `Guardrails` | *guardrails.Engine | Input and output validation rules |
| `SessionState` | map[string]any | Persistent cross-turn state |
| `OutputSchema` | map[string]any | JSON Schema for structured output |
| `NumHistoryRuns` | int | Number of past runs to inject into context |
| `ContextCfg` | ContextConfig | Context window and summarization settings |
| `SystemPrompt` | string | Base system prompt |
| `Instructions` | []string | Additional system instructions |
| `SubAgents` | []*Agent | Child agents for multi-agent orchestration |
| `Capabilities` | []string | Advertised capabilities for the protocol bus |

## Builder API

Use `agent.New(id, name)` to create a builder. All methods return `*Builder` for chaining.

### Core Configuration

```go
a, err := agent.New("my-agent", "My Agent").
    Description("A helpful assistant for technical questions").
    WithUserID("user-123").
    WithModel(model.NewOpenAI(apiKey)).
    WithStorage(store).
    WithMemory(memoryStore).
    WithKnowledge(kb).
    WithMemoryManager(mgr).
    WithOutputSchema(schema).
    WithHistoryRuns(3).
    WithContextConfig(agent.ContextConfig{
        MaxContextTokens:    128000,
        SummarizeThreshold:  0.8,
        PreserveRecentTurns: 5,
    }).
    WithSystemPrompt("You are a senior engineer.").
    AddInstruction("Always cite sources when possible.").
    AddCapability("chat").
    Build()
```

### Builder Methods

| Method | Description |
|--------|-------------|
| `Description(d string)` | Set agent description |
| `WithUserID(id string)` | Set user scope |
| `WithModel(p model.Provider)` | Set LLM provider |
| `WithStorage(s storage.Storage)` | Set persistence backend |
| `WithMemory(m *memory.Store)` | Set memory store |
| `WithKnowledge(k knowledge.Knowledge)` | Set RAG knowledge base |
| `WithMemoryManager(m *memory.Manager)` | Set LLM-powered memory manager |
| `WithOutputSchema(s map[string]any)` | Set JSON Schema for structured output |
| `WithHistoryRuns(n int)` | Set number of past runs to inject |
| `WithContextConfig(cfg ContextConfig)` | Set context window and summarization |
| `WithSystemPrompt(prompt string)` | Set base system prompt |
| `AddInstruction(instruction string)` | Append system instruction |
| `AddCapability(capability string)` | Add advertised capability |
| `AddTool(def *tool.Definition)` | Register a tool |
| `AddSkill(s *skill.Skill)` | Register a skill |
| `AddSubAgent(sub *Agent)` | Add child agent |
| `AddHook(h hooks.Hook)` | Add execution hook |
| `AddInputGuardrail(name string, g guardrails.Guardrail)` | Add input validation |
| `AddOutputGuardrail(name string, g guardrails.Guardrail)` | Add output validation |
| `WithGraph(g *graph.StateGraph)` | Set workflow graph |
| `Build()` | Compile and return `*Agent` |

## Chat (Single Turn)

`Chat` sends a single user message and returns the model response. It is stateless: no session is created, and conversation history is not persisted.

The agent builds messages from: system prompt, instructions, long-term memories (via MemoryManager), relevant knowledge (via RAG), and the user message. It runs input guardrails, calls the model, handles tool calls automatically, checks output guardrails, and extracts memories.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/chronos-ai/chronos/engine/model"
    "github.com/chronos-ai/chronos/sdk/agent"
)

func main() {
    ctx := context.Background()

    a, err := agent.New("chat-agent", "Chat Agent").
        WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
        WithSystemPrompt("You are a helpful assistant.").
        Build()
    if err != nil {
        log.Fatal(err)
    }

    resp, err := a.Chat(ctx, "What is the capital of France?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Content)
}
```

## ChatWithSession (Multi-Turn)

`ChatWithSession` maintains a persistent, multi-turn conversation. Messages are stored in the event ledger. When the context window approaches its limit, older messages are automatically summarized to stay within budget.

Requires `Storage` and `Model`. The session is created on first use if it does not exist.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/chronos-ai/chronos/engine/model"
    "github.com/chronos-ai/chronos/sdk/agent"
    "github.com/chronos-ai/chronos/storage/adapters/sqlite"
)

func main() {
    ctx := context.Background()

    store, err := sqlite.New("session.db")
    if err != nil {
        log.Fatal(err)
    }
    defer store.Close()
    if err := store.Migrate(ctx); err != nil {
        log.Fatal(err)
    }

    a, err := agent.New("session-agent", "Session Agent").
        WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
        WithStorage(store).
        WithSystemPrompt("You are a helpful assistant.").
        WithContextConfig(agent.ContextConfig{
            SummarizeThreshold:  0.8,
            PreserveRecentTurns: 5,
        }).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    sessionID := "user-123-conv-1"

    // First turn
    resp1, err := a.ChatWithSession(ctx, sessionID, "My name is Alice.")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp1.Content)

    // Second turn (agent remembers context)
    resp2, err := a.ChatWithSession(ctx, sessionID, "What is my name?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp2.Content)
}
```

## Run (Graph-Based Execution)

`Run` executes a StateGraph with checkpointing. Each node receives and returns `graph.State` (a `map[string]any`). The runner persists checkpoints to storage, enabling resume after interrupts.

Requires `Graph` and `Storage`.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/chronos-ai/chronos/engine/graph"
    "github.com/chronos-ai/chronos/sdk/agent"
    "github.com/chronos-ai/chronos/storage/adapters/sqlite"
)

func main() {
    ctx := context.Background()

    store, err := sqlite.New("run.db")
    if err != nil {
        log.Fatal(err)
    }
    defer store.Close()
    if err := store.Migrate(ctx); err != nil {
        log.Fatal(err)
    }

    g := graph.New("workflow").
        AddNode("greet", func(_ context.Context, s graph.State) (graph.State, error) {
            s["greeting"] = fmt.Sprintf("Hello, %s!", s["user"])
            return s, nil
        }).
        AddNode("classify", func(_ context.Context, s graph.State) (graph.State, error) {
            s["intent"] = "general_question"
            return s, nil
        }).
        AddNode("respond", func(_ context.Context, s graph.State) (graph.State, error) {
            s["response"] = fmt.Sprintf("Intent: %s. How can I help?", s["intent"])
            return s, nil
        }).
        SetEntryPoint("greet").
        AddEdge("greet", "classify").
        AddEdge("classify", "respond").
        SetFinishPoint("respond")

    a, err := agent.New("run-agent", "Run Agent").
        WithStorage(store).
        WithGraph(g).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    result, err := a.Run(ctx, map[string]any{"user": "World"})
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Result: %v\n", result.State)
}
```

## Resume (Continue from Checkpoint)

`Resume` continues a paused session from its last checkpoint. Use this when a graph contains an interrupt node that requires human approval, or when execution was stopped for any reason.

```go
result, err := a.Resume(ctx, sessionID)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Resumed result: %v\n", result.State)
```

## ContextConfig

`ContextConfig` controls context window management and automatic summarization in `ChatWithSession`:

| Field | Type | Description |
|-------|------|--------------|
| `MaxContextTokens` | int | Override model default; 0 = use model default |
| `SummarizeThreshold` | float64 | Fraction of context window to trigger summarization (default 0.8) |
| `PreserveRecentTurns` | int | Number of recent user/assistant pairs to keep (default 5) |
