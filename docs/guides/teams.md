---
title: "Multi-Agent Teams"
permalink: /guides/teams/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
---

Teams orchestrate multiple agents working together. Chronos supports four strategies: sequential (state flows through agents in order), parallel (agents run concurrently, results merged), router (a function selects one agent), and coordinator (first agent decomposes and delegates to specialists). Agents communicate via the Protocol Bus.

## Creating a Team

```go
t := team.New(id, name, strategy)
```

| Strategy | Description |
|----------|-------------|
| `StrategySequential` | Agents run in order; state flows through each |
| `StrategyParallel` | Agents run concurrently; results merged |
| `StrategyRouter` | A function selects which agent handles the input |
| `StrategyCoordinator` | First agent decomposes; delegates to specialists via bus |

## Adding Agents

```go
t.AddAgent(researcherAgent)
t.AddAgent(writerAgent)
t.AddAgent(reviewerAgent)
```

Agents are registered on the Protocol Bus when added. Order matters for sequential and coordinator strategies.

## Sequential Strategy

Agents run one after another. State flows from each agent to the next; results accumulate in `SharedContext`.

```go
t := team.New("pipeline", "Research Pipeline", team.StrategySequential)
t.AddAgent(researcher)
t.AddAgent(writer)
t.AddAgent(reviewer)

result, err := t.Run(ctx, graph.State{"topic": "Climate change"})
// researcher runs first, then writer, then reviewer
// result.State contains merged output
```

## Parallel Strategy

Agents run concurrently. Results are merged via `SetMerge` or by combining all state keys.

```go
t := team.New("parallel", "Parallel Team", team.StrategyParallel)
t.AddAgent(agentA)
t.AddAgent(agentB)
t.AddAgent(agentC)

t.SetMerge(func(results []graph.State) graph.State {
    merged := make(graph.State)
    for _, r := range results {
        for k, v := range r {
            merged[k] = v
        }
    }
    return merged
})

result, err := t.Run(ctx, graph.State{"query": "Analyze X"})
```

Without `SetMerge`, the default merge combines all state keys (later values overwrite earlier for duplicate keys).

## Router Strategy

A routing function selects which agent handles the input.

```go
t := team.New("router", "Router Team", team.StrategyRouter)
t.AddAgent(techAgent)
t.AddAgent(supportAgent)
t.AddAgent(salesAgent)

t.SetRouter(func(state graph.State) string {
    topic, _ := state["topic"].(string)
    switch {
    case strings.Contains(topic, "bug") || strings.Contains(topic, "error"):
        return supportAgent.ID
    case strings.Contains(topic, "pricing") || strings.Contains(topic, "buy"):
        return salesAgent.ID
    default:
        return techAgent.ID
    }
})

result, err := t.Run(ctx, graph.State{"topic": "How do I fix this error?", "message": "..."})
```

## Coordinator Strategy

The first agent acts as coordinator: it decomposes the task and delegates sub-tasks to specialists via the Protocol Bus.

```go
t := team.New("coord", "Coordinator Team", team.StrategyCoordinator)
t.AddAgent(coordinatorAgent)  // must be first
t.AddAgent(researcherAgent)
t.AddAgent(writerAgent)

result, err := t.Run(ctx, graph.State{"task": "Write a report on renewable energy"})
// Coordinator runs, produces plan, delegates to researcher and writer via bus
```

## Protocol Bus

The bus routes typed messages between agents. Each team gets a `Bus` when created.

### Register

Agents are registered when added via `AddAgent`. For manual registration:

```go
bus.Register(id, name, description, capabilities, handler)
```

### Message Types

| Type | Description |
|------|-------------|
| `task_request` | Ask another agent to perform work |
| `task_result` | Outcome of a delegated task |
| `question` | Ask another agent for information |
| `answer` | Response to a question |
| `broadcast` | Send update to all agents |
| `handoff` | Transfer conversation/task |

### DelegateTask

Send a task and wait for the result:

```go
result, err := t.DelegateTask(ctx, fromAgentID, toAgentID, "subtask", protocol.TaskPayload{
    Description: "Analyze the data",
    Input:       map[string]any{"data": data},
})
```

### Ask

Ask a question and wait for an answer (use the bus directly):

```go
answer, err := t.Bus.Ask(ctx, fromAgentID, toAgentID, "What is the capital of France?")
```

### FindByCapability

Find agents that advertise a capability:

```go
peers := t.Bus.FindByCapability("code_review")
```

### Broadcast

Send a message to all agents (except sender):

```go
err := t.Broadcast(ctx, fromAgentID, "status_update", map[string]any{
    "progress": 0.5,
    "message":  "Halfway done",
})
```

## Direct Agent-to-Agent Communication

When an agent delegates a task, the bus delivers a `TypeTaskRequest` envelope. The team's handler invokes the target agent's `Run` with the task payload. The agent receives `_task_description` and `_delegated_by` in its input state. The handler returns a `TypeTaskResult` envelope with success/failure and output.

Agents can also use the bus directly (if they have a reference) to send questions, broadcast updates, or hand off conversations.

## Code Examples

### Sequential

```go
researcher, _ := agent.New("researcher", "Researcher").WithModel(p).WithGraph(researchGraph).Build()
writer, _ := agent.New("writer", "Writer").WithModel(p).WithGraph(writeGraph).Build()

t := team.New("seq", "Sequential", team.StrategySequential)
t.AddAgent(researcher)
t.AddAgent(writer)

result, err := t.Run(ctx, graph.State{"topic": "AI ethics"})
```

### Parallel

```go
t := team.New("par", "Parallel", team.StrategyParallel)
t.AddAgent(agent1)
t.AddAgent(agent2)
t.SetMerge(func(results []graph.State) graph.State {
    merged := make(graph.State)
    for _, r := range results {
        for k, v := range r {
            merged[k] = v
        }
    }
    return merged
})
result, err := t.Run(ctx, graph.State{"input": "..."})
```

### Router

```go
t := team.New("router", "Router", team.StrategyRouter)
t.AddAgent(techAgent)
t.AddAgent(supportAgent)
t.SetRouter(func(s graph.State) string {
    if msg, ok := s["message"].(string); ok && strings.Contains(msg, "bug") {
        return supportAgent.ID
    }
    return techAgent.ID
})
result, err := t.Run(ctx, graph.State{"message": "I found a bug in..."})
```

### Coordinator

```go
coord, _ := agent.New("coord", "Coordinator").WithModel(p).WithGraph(coordGraph).Build()
spec1, _ := agent.New("spec1", "Specialist 1").WithModel(p).Build()
spec2, _ := agent.New("spec2", "Specialist 2").WithModel(p).Build()

t := team.New("coord", "Coordinator", team.StrategyCoordinator)
t.AddAgent(coord)
t.AddAgent(spec1)
t.AddAgent(spec2)

result, err := t.Run(ctx, graph.State{"task": "Write a technical brief"})
```
