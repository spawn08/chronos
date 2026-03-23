# Quickstart

This guide walks you through building your first Chronos agent in under 5 minutes. No API keys required.

## 1. Minimal Graph Agent

The simplest Chronos agent uses SQLite for persistence and a StateGraph for deterministic logic:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/spawn08/chronos/engine/graph"
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/storage/adapters/sqlite"
)

func main() {
    ctx := context.Background()

    store, _ := sqlite.New(":memory:")
    defer store.Close()
    store.Migrate(ctx)

    g := graph.New("hello").
        AddNode("greet", func(_ context.Context, s graph.State) (graph.State, error) {
            s["message"] = fmt.Sprintf("Hello, %s!", s["user"])
            return s, nil
        }).
        SetEntryPoint("greet").
        SetFinishPoint("greet")

    a, _ := agent.New("hello-agent", "Hello Agent").
        WithStorage(store).
        WithGraph(g).
        Build()

    result, _ := a.Run(ctx, map[string]any{"user": "World"})
    fmt.Println(result.State["message"]) // Hello, World!
}
```

## 2. Chat Agent (with LLM)

Connect to any provider to get LLM-powered responses:

```go
a, _ := agent.New("chat-agent", "Chat Agent").
    WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
    WithSystemPrompt("You are a helpful assistant.").
    Build()

resp, _ := a.Chat(ctx, "What is Go?")
fmt.Println(resp.Content)
```

Swap `NewOpenAI` with `NewAnthropic`, `NewGemini`, `NewOllama`, or any other provider — the API is identical.

## 3. Agent with Tools

Register tools the LLM can call:

```go
a, _ := agent.New("tool-agent", "Tool Agent").
    WithModel(model.NewOpenAI(key)).
    AddTool(&tool.Definition{
        Name:        "calculate",
        Description: "Perform arithmetic",
        Permission:  tool.PermAllow,
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "expression": map[string]any{"type": "string"},
            },
        },
        Handler: func(_ context.Context, args map[string]any) (any, error) {
            // Your calculation logic here
            return "42", nil
        },
    }).
    Build()
```

## 4. Multi-Turn Sessions

Persistent conversations across multiple turns with automatic context management:

```go
store, _ := sqlite.New("sessions.db")
store.Migrate(ctx)

a, _ := agent.New("session-agent", "Session Agent").
    WithModel(provider).
    WithStorage(store).
    Build()

// Same session ID = continuous conversation
a.ChatWithSession(ctx, "session-1", "My name is Alice")
a.ChatWithSession(ctx, "session-1", "What is my name?")
// Agent remembers: "Your name is Alice"
```

## 5. Local Models (No API Key)

Use Ollama for fully local inference:

```bash
# Start Ollama
ollama serve
ollama pull llama3.2
```

```go
a, _ := agent.New("local-agent", "Local Agent").
    WithModel(model.NewOllama("http://localhost:11434", "llama3.2")).
    Build()

resp, _ := a.Chat(ctx, "Explain goroutines")
```

## Next Steps

- [Examples Guide](../guides/examples.md) — All 12+ runnable examples
- [Model Providers](../reference/providers.md) — All supported LLM providers
- [Architecture](../reference/architecture.md) — System design and layers
