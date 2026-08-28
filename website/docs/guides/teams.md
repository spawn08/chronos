---
title: "Multi-Agent Teams"
---


# Multi-Agent Teams

Chronos supports six team orchestration strategies for composing agents into collaborative workflows: sequential, parallel, router, coordinator, swarm, and hierarchy.

The snippets on this page assume these imports, plus the agents built in [Building Agents for Teams](#building-agents-for-teams) below:

```go
import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "strings"

    "github.com/spawn08/chronos/engine/graph"
    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/sdk/protocol"
    "github.com/spawn08/chronos/sdk/team"
)
```

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

### Swarm

Agents hand off directly to each other with no central coordinator. `team.NewSwarm` wires a `transfer_to_<agent_id>` tool into every agent for every other agent in the swarm, then compiles an internal `graph.StateGraph` that routes to whichever agent a handoff tool call names.

```go
sw, err := team.NewSwarm(team.SwarmConfig{
    Agents:       []*agent.Agent{triage, billing, technical},
    InitialAgent: "triage", // defaults to Agents[0] when empty
    MaxHandoffs:  6,        // defaults to 10 when <= 0
})
if err != nil {
    log.Fatal(err)
}

result, err := sw.Run(ctx, graph.State{
    "message": "My invoice looks wrong and the app also crashes on login",
})
if err != nil {
    log.Fatal(err)
}
fmt.Println(result["output"], result["last_agent"])
```

`NewSwarm` requires at least 2 agents. Each agent's model decides whether to answer directly or call a transfer tool to hand off; `state["last_agent"]` and `state["output"]` track where control ended up.

### Hierarchy

Multi-level supervision: a root supervisor delegates to worker agents and/or nested `SupervisorNode` sub-teams, each managed by its own supervisor agent.

```go
h, err := team.NewHierarchy(team.HierarchyConfig{
    Root: &team.SupervisorNode{
        Supervisor: rootSupervisor,
        Workers:    []*agent.Agent{writer, editor},
        SubTeams: []*team.SupervisorNode{
            {
                Supervisor: researchLead,
                Workers:    []*agent.Agent{researcher},
            },
        },
    },
})
if err != nil {
    log.Fatal(err)
}

result, err := h.Run(ctx, graph.State{"message": "Produce a market analysis report"})
if err != nil {
    log.Fatal(err)
}
fmt.Println(result["output"])
```

`HierarchyConfig.Root` (and its `Supervisor` field) is required. Each supervisor node fans out to its `Workers` and recurses into `SubTeams`; a node with neither is treated as a graph leaf (`SetFinishPoint`).

## Agent Communication

### Protocol Bus

All team agents share a message bus for typed communication:

```go
// Delegate a task from one agent to another
result, err := t.DelegateTask(ctx, "researcher", "writer", "write-draft",
    protocol.TaskPayload{
        Description: "Write a 500-word summary",
        Input: map[string]any{"topic": "AI ethics"},
    })
if err != nil {
    log.Fatal(err)
}
fmt.Println(result.Output)
```

### Direct Channels

For low-latency point-to-point messaging that bypasses the bus:

```go
dc := t.DirectChannel("researcher", "writer", 128)

// researcher sends directly to writer
body, _ := json.Marshal(map[string]any{"topic": "AI ethics", "summary": "..."})
dc.AtoB <- &protocol.Envelope{
    Type:    protocol.TypeTaskResult,
    From:    "researcher",
    To:      "writer",
    Subject: "findings",
    Body:    body,
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

Agents in teams are lightweight — they typically need only a model and system prompt (no graph or storage required). This self-contained example builds every named agent (`researcher`, `writer`, `editor`, `optimist`, `pessimist`, `realist`, `billing`, `technical`, `general`, `triage`, `reviewer`, `rootSupervisor`, `researchLead`) and the `provider` and `ctx` referenced by the strategy snippets above.

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
)

// buildAgent constructs a lightweight team member: just a model and a system
// prompt (no graph or storage required).
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

func main() {
    ctx := context.Background()
    provider := model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

    // Sequential pipeline
    researcher := buildAgent("researcher", "Researcher", "Gathers information", "You are a meticulous researcher.", []string{"research"}, provider)
    writer := buildAgent("writer", "Writer", "Drafts content", "You are a clear, engaging writer.", []string{"writing"}, provider)
    editor := buildAgent("editor", "Editor", "Polishes drafts", "You are a precise copy editor.", []string{"editing"}, provider)

    // Parallel analysis
    optimist := buildAgent("optimist", "Optimist", "Sees the upside", "Analyze from an optimistic perspective.", nil, provider)
    pessimist := buildAgent("pessimist", "Pessimist", "Sees the risk", "Analyze from a critical, risk-focused perspective.", nil, provider)
    realist := buildAgent("realist", "Realist", "Balances both", "Analyze from a balanced, pragmatic perspective.", nil, provider)

    // Router support desk
    billing := buildAgent("billing", "Billing", "Handles invoices", "You handle billing questions.", []string{"invoice"}, provider)
    technical := buildAgent("technical", "Technical", "Handles bugs", "You handle technical support.", []string{"error"}, provider)
    general := buildAgent("general", "General", "Handles everything else", "You handle general inquiries.", nil, provider)

    // Swarm / hierarchy members
    triage := buildAgent("triage", "Triage", "Routes to the right specialist", "Understand the request and hand off to billing or technical as needed.", nil, provider)
    reviewer := buildAgent("reviewer", "Reviewer", "Reviews drafts", "You review drafts for accuracy and tone.", []string{"review"}, provider)
    rootSupervisor := buildAgent("root-supervisor", "Root Supervisor", "Top-level coordinator", "Delegate work to your team and sub-teams.", nil, provider)
    researchLead := buildAgent("research-lead", "Research Lead", "Manages the research sub-team", "Delegate research tasks to your team.", nil, provider)

    fmt.Println(researcher.ID, writer.ID, editor.ID, optimist.ID, pessimist.ID, realist.ID,
        billing.ID, technical.ID, general.ID, triage.ID, reviewer.ID, rootSupervisor.ID, researchLead.ID)
    _ = ctx
    // Continue with any of the team strategies described above, e.g.:
    // t := team.New("pipeline", "Content Pipeline", team.StrategySequential).
    //     AddAgent(researcher).AddAgent(writer).AddAgent(editor)
}
```

Capabilities are used by the router strategy's fallback capability matcher when no explicit router function is set.
