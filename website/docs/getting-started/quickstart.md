---
title: "Quickstart"
---


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
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
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

    resp, err := a.Chat(ctx, "What is Go?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Content)
}
```

Swap `NewOpenAI` with `NewAnthropic`, `NewGemini`, `NewOllama`, or any other provider — the API is identical.

## 3. Agent with Tools

Register tools the LLM can call:

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/engine/tool"
    "github.com/spawn08/chronos/sdk/agent"
)

func main() {
    a, err := agent.New("tool-agent", "Tool Agent").
        WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
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
    if err != nil {
        log.Fatal(err)
    }
    _ = a
}
```

## 4. Multi-Turn Sessions

Persistent conversations across multiple turns with automatic context management:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/storage/adapters/sqlite"
)

func main() {
    ctx := context.Background()

    store, err := sqlite.New("sessions.db")
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
        Build()
    if err != nil {
        log.Fatal(err)
    }

    // Same session ID = continuous conversation
    a.ChatWithSession(ctx, "session-1", "My name is Alice")
    resp, _ := a.ChatWithSession(ctx, "session-1", "What is my name?")
    fmt.Println(resp.Content)
    // Agent remembers: "Your name is Alice"
}
```

## 5. Local Models (No API Key)

Use Ollama for fully local inference:

```bash
# Start Ollama
ollama serve
ollama pull llama3.2
```

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
)

func main() {
    ctx := context.Background()

    a, err := agent.New("local-agent", "Local Agent").
        WithModel(model.NewOllama("http://localhost:11434", "llama3.2")).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    resp, err := a.Chat(ctx, "Explain goroutines")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Content)
}
```

## Next Steps

- [Examples Guide](../guides/examples.md) — All 20+ runnable examples
- [Model Providers](../reference/providers.md) — All supported LLM providers
- [Architecture](../reference/architecture.mdx) — System design and layers
