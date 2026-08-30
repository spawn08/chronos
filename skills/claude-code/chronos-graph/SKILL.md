---
name: chronos-graph
description: Build durable StateGraph workflows with Chronos — linear pipelines, conditional branches, retry loops, and human-in-the-loop interrupts. Each pattern has ready-to-copy Go code in examples/.
---

# Chronos StateGraph Workflows

## Activation
Use this skill when:
- Building graph-based agent workflows
- Developer needs nodes, edges, conditions, or interrupt patterns
- Creating human-in-the-loop approval flows
- Setting up checkpointing and resume after pause/crash

## Pattern Selection

| Pattern | Directory | Flow | When to use |
|---------|-----------|------|-------------|
| **Linear** | `examples/linear/` | A → B → C | Simple step-by-step pipeline |
| **Conditional** | `examples/conditional/` | A → if/else → B or C | Decision routing based on state |
| **Human-in-the-Loop** | `examples/human-in-the-loop/` | A → pause → resume → B | Approval gates, manual review |
| **Retry Loop** | `examples/retry-loop/` | A → check → retry or done | Quality checks with re-execution |

---

## Pattern: Linear Pipeline

**Files:** `examples/linear/main.go`

```go
g := graph.New("pipeline")
g.AddNode("fetch", fetchFn)
g.AddNode("process", processFn)
g.AddNode("output", outputFn)
g.AddEdge("fetch", "process")
g.AddEdge("process", "output")
g.SetEntryPoint("fetch")
g.SetFinishPoint("output")
```

---

## Pattern: Conditional Branch

**Files:** `examples/conditional/main.go`

```go
g := graph.New("conditional")
g.AddNode("analyze", analyzeFn)
g.AddNode("approve", approveFn)
g.AddNode("reject", rejectFn)

g.AddConditionalEdge("analyze", func(state graph.State) string {
    score, _ := state["score"].(float64)
    if score >= 0.8 {
        return "approve"
    }
    return "reject"
})

g.SetEntryPoint("analyze")
g.SetFinishPoint("approve")
g.SetFinishPoint("reject")
```

---

## Pattern: Human-in-the-Loop

**Files:** `examples/human-in-the-loop/main.go`

`AddInterruptNode` pauses execution and checkpoints state. Resume later with the session ID.

```go
g := graph.New("approval-flow")
g.AddNode("prepare", prepareFn)
g.AddInterruptNode("review", func(ctx context.Context, state graph.State) (graph.State, error) {
    state["review_prompt"] = "Please approve this action"
    return state, nil
})
g.AddNode("execute", executeFn)

g.AddEdge("prepare", "review")
g.AddEdge("review", "execute")
g.SetEntryPoint("prepare")
g.SetFinishPoint("execute")

// Run — pauses at "review"
result, _ := a.Run(ctx, graph.State{"input": "data"})
// result.Status == "interrupted"

// ... human approves ...

// Resume from where it paused
result, _ = a.Resume(ctx, result.SessionID)
// result.Status == "completed"
```

---

## Pattern: Retry Loop

**Files:** `examples/retry-loop/main.go`

```go
g := graph.New("retry")
g.AddNode("generate", generateFn)
g.AddNode("validate", validateFn)
g.AddNode("done", doneFn)

g.AddConditionalEdge("validate", func(state graph.State) string {
    passed, _ := state["valid"].(bool)
    retries, _ := state["retries"].(int)
    if passed || retries >= 3 {
        return "done"
    }
    state["retries"] = retries + 1
    return "generate" // retry
})

g.AddEdge("generate", "validate")
g.SetEntryPoint("generate")
g.SetFinishPoint("done")
```

---

## Core API Reference

### Types
```go
import "github.com/spawn08/chronos/engine/graph"

type State = map[string]any
type NodeFunc = func(ctx context.Context, state State) (State, error)
type EdgeCondition = func(state State) string
```

### StateGraph Builder
```go
g := graph.New("graph-id")

g.AddNode("name", nodeFn)                    // regular node
g.AddInterruptNode("name", nodeFn)            // interrupt (pauses)
g.AddEdge("from", "to")                       // fixed edge
g.AddConditionalEdge("from", conditionFn)      // conditional routing
g.SetEntryPoint("nodeID")                      // starting node
g.SetFinishPoint("nodeID")                     // terminal node (multiple OK)

compiled, err := g.Compile()                   // validates structure
```

### Runner
```go
runner := graph.NewRunner(compiled, store)
runner = runner.WithBroker(broker)    // optional streaming
runner = runner.WithTracer(tracer)    // optional tracing

result, err := runner.Run(ctx, "session-id", initialState)
result, err = runner.Resume(ctx, "session-id")

// result.State    — final state map
// result.Status   — "completed" or "interrupted"
// result.SessionID
```

### With Agent
```go
a, _ := agent.New("id", "name").
    WithModel(llm).
    WithStorage(store).
    WithGraph(g).
    Build()

result, _ := a.Run(ctx, graph.State{"input": "query"})
result, _ = a.Resume(ctx, sessionID)
```

### Writing Node Functions
```go
func myNode(ctx context.Context, state graph.State) (graph.State, error) {
    input, _ := state["input"].(string)
    // ... process ...
    state["output"] = result
    return state, nil
    // Return error to abort: return state, fmt.Errorf("failed: %w", err)
}
```

## YAML Graphs (no Go code required)

Every pattern above can also be declared in an agent's YAML config via a `graph:` block, instead of Go's `graph.New(...).AddNode(...)` builder. This is the same durable graph underneath — `Agent.Run`/`Resume` behave identically — just expressed declaratively. Node `type` is one of `model` (calls the agent's LLM with a templated prompt), `tool` (calls a registered tool), `subagent` (delegates to another agent in the same file), or `passthrough` (merges a static `set:` map into state — the building block for an interrupt gate).

**Linear pipeline + human-in-the-loop**, the YAML equivalent of the two patterns above:
```yaml
agents:
  - id: approval-flow
    model: {provider: openai, model: gpt-4o}
    storage: {backend: sqlite, dsn: chronos.db}
    durable: true   # registers this graph with `chronos serve`'s dashboard
    graph:
      entry: prepare
      finish: execute
      nodes:
        - {id: prepare, type: tool, tool: prepare_action}
        - {id: review, type: passthrough, interrupt: true, set: {reviewed: true}}
        - {id: execute, type: tool, tool: execute_action}
      edges:
        - {from: prepare, to: review}
        - {from: review, to: execute}
```

**Conditional branch** — routes on a state key instead of a Go `EdgeCondition` closure:
```yaml
    graph:
      entry: analyze
      nodes:
        - {id: analyze, type: model, prompt: "Score this: {{.state.input}}", output_key: score}
        - {id: approve, type: passthrough, set: {decision: approved}}
        - {id: reject, type: passthrough, set: {decision: rejected}}
      edges:
        - from: analyze
          conditional: true
          route_key: score
          routes: {high: approve, low: reject}
          default: reject
```

A dynamic retry-loop with a counter (`state["retries"]++`) has no pure-YAML equivalent — `passthrough` only sets static values — so keep that pattern in Go, or back a `tool` node with a custom Go handler (`WithToolHandler`) that does the counting.

Full schema reference, `subagent` delegation, and `chronos serve`/dashboard integration: see `website/docs/guides/yaml-dashboard.md` and the runnable `examples/yaml_dashboard/`.

## Key Concepts
- **State** is a `map[string]any` flowing through nodes — add/modify keys as data flows
- **Checkpoint**: state is persisted at each node, enabling crash recovery
- **Interrupt**: `AddInterruptNode` pauses and saves — resume later with `Resume(ctx, sessionID)`
- **Compile**: validates the graph (reachability, entry/finish points)
- **Session**: each run gets a unique session — resume by session ID
