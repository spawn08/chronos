---
title: "Multi-Agent Teams"
permalink: /guides/teams/
sidebar:
  nav: "docs"
---

# Multi-Agent Teams

Chronos supports four team orchestration strategies for composing agents into collaborative workflows.

## Team Strategies

### Sequential

Agents run one after another in a pipeline. Each agent's output becomes the next agent's input.

```go
t := team.New("pipeline", "Content Pipeline", team.StrategySequential).
    AddAgent(researcher).
    AddAgent(writer).
    AddAgent(editor)

result, _ := t.Run(ctx, graph.State{
    "message": "Write about AI in healthcare",
})
```

The sequential strategy automatically passes the `response` from each agent as part of the state to the next.

### Parallel

All agents run concurrently with bounded concurrency. Results are merged using a configurable merge function.

```go
t := team.New("analysis", "Multi-Perspective Analysis", team.StrategyParallel).
    AddAgent(optimist).
    AddAgent(pessimist).
    AddAgent(realist).
    SetMaxConcurrency(2).
    SetErrorStrategy(team.ErrorStrategyBestEffort).
    SetMerge(func(results []graph.State) graph.State {
        merged := make(graph.State)
        for i, r := range results {
            merged[fmt.Sprintf("perspective_%d", i)] = r["response"]
        }
        return merged
    })
```

Error strategies:
- `ErrorStrategyFailFast` — abort on first error (default)
- `ErrorStrategyBestEffort` — continue despite failures, merge what succeeds

### Router

Dispatches the input to a single agent based on a routing function.

```go
t := team.New("support", "Support Router", team.StrategyRouter).
    AddAgent(billing).
    AddAgent(technical).
    AddAgent(general).
    SetRouter(func(state graph.State) string {
        msg, _ := state["message"].(string)
        if strings.Contains(msg, "invoice") { return "billing" }
        if strings.Contains(msg, "error")   { return "technical" }
        return "general"
    })
```

If no router is set, Chronos falls back to capability matching against each agent's capabilities.

### Coordinator

An LLM-powered supervisor decomposes complex tasks into subtasks and delegates to specialist agents. The coordinator can iterate, reviewing results and re-planning.

```go
supervisor, _ := agent.New("supervisor", "Supervisor").
    WithModel(provider).
    WithSystemPrompt("Break tasks into steps and delegate to specialists.").
    Build()

t := team.New("project", "Project Team", team.StrategyCoordinator).
    SetCoordinator(supervisor).
    AddAgent(researcher).
    AddAgent(writer).
    AddAgent(reviewer).
    SetMaxIterations(3)
```

The coordinator receives a JSON plan prompt and produces task assignments. After each round, it reviews results and decides whether to continue or finish.

## Agent Communication

### Protocol Bus

All team agents share a message bus for typed communication:

```go
// Delegate a task from one agent to another
result, _ := t.DelegateTask(ctx, "researcher", "writer", "write-draft",
    protocol.TaskPayload{
        Description: "Write a 500-word summary",
        Input: map[string]any{"topic": "AI ethics"},
    })
```

### Direct Channels

For low-latency point-to-point messaging that bypasses the bus:

```go
dc := t.DirectChannel("researcher", "writer", 128)

// researcher sends directly to writer
dc.AtoB <- &protocol.Envelope{
    Type:    protocol.TypeTaskResult,
    From:    "researcher",
    To:      "writer",
    Subject: "findings",
    Body:    jsonBytes,
}

// writer receives
msg := <-dc.AtoB
```

### Broadcast

Send a message to all agents in the team:

```go
t.Broadcast(ctx, "coordinator", "status_update", map[string]any{
    "phase": "review",
    "progress": 0.75,
})
```

## Building Agents for Teams

Agents in teams are lightweight — they typically need only a model and system prompt (no graph or storage required):

```go
func buildAgent(id, name, desc, prompt string, caps []string, provider model.Provider) *agent.Agent {
    b := agent.New(id, name).
        Description(desc).
        WithModel(provider).
        WithSystemPrompt(prompt)
    for _, c := range caps {
        b.AddCapability(c)
    }
    a, _ := b.Build()
    return a
}
```

Capabilities are used by the router strategy's fallback capability matcher when no explicit router function is set.
