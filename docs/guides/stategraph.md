---
title: "StateGraph Runtime"
permalink: /guides/stategraph/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
---

The StateGraph runtime executes durable, checkpointed workflows. Nodes run in sequence or branch via conditional edges. State flows through the graph as a `map[string]any`. Checkpointing after every node enables resume after interrupts and time-travel debugging.

## Creating a Graph

```go
g := graph.New("my-workflow")
```

## Adding Nodes

Each node has an ID and a handler function:

```go
g.AddNode("greet", func(ctx context.Context, s graph.State) (graph.State, error) {
    s["greeting"] = fmt.Sprintf("Hello, %s!", s["user"])
    return s, nil
})

g.AddNode("classify", func(ctx context.Context, s graph.State) (graph.State, error) {
    s["intent"] = "general_question"
    return s, nil
})
```

### Interrupt Nodes

Interrupt nodes pause execution for human-in-the-loop approval. The runner checkpoints and returns before executing the node.

```go
g.AddInterruptNode("approve", func(ctx context.Context, s graph.State) (graph.State, error) {
    // This node runs only after Resume; before that, execution pauses
    s["approved"] = true
    return s, nil
})
```

## Entry and Finish Points

```go
g.SetEntryPoint("greet")   // Start here
g.SetFinishPoint("respond") // End here
```

`SetEntryPoint` adds an edge from `__start__` to the given node. `SetFinishPoint` adds an edge from the given node to `__end__`.

## Edges

### Static Edges

```go
g.AddEdge("greet", "classify")
g.AddEdge("classify", "respond")
```

### Conditional Edges

Route based on state. The condition function returns the target node ID.

```go
g.AddConditionalEdge("classify", func(s graph.State) string {
    intent, _ := s["intent"].(string)
    switch intent {
    case "support":
        return "support_flow"
    case "sales":
        return "sales_flow"
    default:
        return "general_flow"
    }
})
```

## Compiling

Validate the graph and produce an immutable `CompiledGraph`:

```go
compiled, err := g.Compile()
if err != nil {
    return err  // e.g., missing entry point, invalid edge targets
}
```

## Runner

The runner executes a compiled graph with checkpointing:

```go
runner := graph.NewRunner(compiled, storage)
```

### Run

Start a new execution with initial state:

```go
result, err := runner.Run(ctx, sessionID, graph.State{"user": "Alice"})
```

### Resume

Continue from the latest checkpoint (e.g., after an interrupt):

```go
result, err := runner.Resume(ctx, sessionID)
```

### ResumeFromCheckpoint

Resume from a specific checkpoint (time-travel debugging):

```go
result, err := runner.ResumeFromCheckpoint(ctx, checkpointID)
```

## Checkpointing

State is saved after every node. Each checkpoint stores:

- Session ID, Run ID, Node ID
- Full state
- Sequence number

Storage must implement `SaveCheckpoint` and `GetLatestCheckpoint` (and `GetCheckpoint` for time-travel). Use SQLite or Postgres adapters.

## StreamEvent

Subscribe to execution events for observability. Use the runner directly (not via `agent.Run`) to access the stream:

```go
compiled, _ := g.Compile()
runner := graph.NewRunner(compiled, store)
stream := runner.Stream()
for evt := range stream {
    switch evt.Type {
    case "node_start":
        fmt.Printf("Starting node %s\n", evt.NodeID)
    case "node_end":
        fmt.Printf("Finished node %s\n", evt.NodeID)
    case "edge_transition":
        fmt.Printf("Transitioning to %s\n", evt.NodeID)
    case "interrupt":
        fmt.Printf("Paused at interrupt node %s\n", evt.NodeID)
    case "error":
        fmt.Printf("Error: %s\n", evt.Error)
    case "completed":
        fmt.Println("Graph completed")
    }
}
```

| Type | Description |
|------|-------------|
| `node_start` | Node execution began |
| `node_end` | Node execution finished |
| `edge_transition` | Transitioning to next node |
| `interrupt` | Paused at interrupt node |
| `error` | Node failed |
| `completed` | Graph finished successfully |

## Integration with Agent

Attach a graph to an agent via the builder. The agent's `Run` and `Resume` methods use the graph:

```go
g := graph.New("workflow").
    AddNode("greet", func(ctx context.Context, s graph.State) (graph.State, error) {
        s["greeting"] = fmt.Sprintf("Hello, %s!", s["user"])
        return s, nil
    }).
    AddNode("respond", func(ctx context.Context, s graph.State) (graph.State, error) {
        s["response"] = "How can I help?"
        return s, nil
    }).
    SetEntryPoint("greet").
    AddEdge("greet", "respond").
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
```

## Time-Travel Debugging

Resume from any historical checkpoint to replay or debug:

```go
checkpoints, _ := store.ListCheckpoints(ctx, sessionID)
// User selects checkpointID from UI or CLI
result, err := runner.ResumeFromCheckpoint(ctx, checkpointID)
```

## Complete Example

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
