---
title: "Quickstart"
permalink: /getting-started/quickstart/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
---

Chronos supports three ways to get started: YAML configuration, the Go builder API, and graph-based agents. Choose the approach that fits your workflow.

## 1. YAML configuration

Define agents in `.chronos/agents.yaml` and use the CLI without writing Go code.

Create `.chronos/agents.yaml`:

```yaml
defaults:
  model:
    provider: openai
    api_key: ${OPENAI_API_KEY}
  storage:
    backend: sqlite
    dsn: chronos.db

agents:
  - id: dev
    name: Dev Agent
    model:
      provider: openai
      model: gpt-4o
      api_key: ${OPENAI_API_KEY}
    system_prompt: |
      You are a senior software engineer. Write clean, well-tested code.
```

Set your API key and start the REPL:

```bash
export OPENAI_API_KEY=sk-...
go run ./cli/main.go repl
```

The REPL loads the first agent from your config. You can also specify an agent by ID:

```bash
go run ./cli/main.go repl --agent dev
```

## 2. Go builder API

Build agents programmatically with the fluent builder:

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

    a, err := agent.New("my-agent", "My Agent").
        WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
        WithSystemPrompt("You are a helpful assistant.").
        Build()
    if err != nil {
        log.Fatal(err)
    }

    resp, err := a.Chat(ctx, "What is 2 + 2?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Content)
}
```

The builder supports tools, guardrails, memory, and more. See the [Agent Builder API](/api/agent-builder/) for full options.

## 3. Graph-based agent

For durable, multi-step workflows with checkpoints and resume, use a StateGraph:

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

    // 1. Open SQLite storage
    store, err := sqlite.New("quickstart.db")
    if err != nil {
        log.Fatal(err)
    }
    defer store.Close()
    if err := store.Migrate(ctx); err != nil {
        log.Fatal(err)
    }

    // 2. Define the graph
    g := graph.New("quickstart").
        AddNode("greet", func(_ context.Context, s graph.State) (graph.State, error) {
            s["greeting"] = fmt.Sprintf("Hello, %s!", s["user"])
            return s, nil
        }).
        AddNode("respond", func(_ context.Context, s graph.State) (graph.State, error) {
            s["response"] = "How can I help?"
            return s, nil
        }).
        SetEntryPoint("greet").
        AddEdge("greet", "respond").
        SetFinishPoint("respond")

    // 3. Build the agent with storage and graph
    a, err := agent.New("quickstart-agent", "Quickstart Agent").
        WithStorage(store).
        WithGraph(g).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    // 4. Run
    result, err := a.Run(ctx, map[string]any{"user": "World"})
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Result: %v\n", result.State)
}
```

Graph nodes receive and return `graph.State` (a `map[string]any`). The runner persists checkpoints to storage, enabling resume after interrupts.

## Loading YAML from Go

Load agent configuration from a YAML file and build agents in code:

```go
package main

import (
    "context"
    "log"

    "github.com/chronos-ai/chronos/sdk/agent"
)

func main() {
    ctx := context.Background()

    // Load config (searches .chronos/agents.yaml, agents.yaml, ~/.chronos/agents.yaml)
    fc, err := agent.LoadFile("")
    if err != nil {
        log.Fatal(err)
    }

    // Find an agent by ID or name
    cfg, err := fc.FindAgent("dev")
    if err != nil {
        log.Fatal(err)
    }

    // Build the agent from config
    a, err := agent.BuildAgent(ctx, cfg)
    if err != nil {
        log.Fatal(err)
    }

    // Use it
    resp, err := a.Chat(ctx, "Hello")
    if err != nil {
        log.Fatal(err)
    }
    log.Println(resp.Content)
}
```

- **`agent.LoadFile("")`** — Loads from the default search path. Pass a path to use a specific file.
- **`fc.FindAgent("dev")`** — Looks up an agent by ID or name (case-insensitive).
- **`agent.BuildAgent(ctx, cfg)`** — Constructs a fully-wired `*Agent` from the config, including model provider, storage, and migrations.
