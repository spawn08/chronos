---
title: "Multi-Agent Teams"
permalink: /guides/teams/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
---

Teams let multiple AI agents collaborate on a shared task. Instead of one agent doing everything, you build specialist agents — a researcher, a writer, a reviewer — and let a team strategy coordinate how they work together.

This guide walks you through every concept from scratch, with complete, runnable examples.

## Before You Start

**Prerequisites:**

- Go 1.24 or later
- An API key from any supported provider (OpenAI, Anthropic, Gemini, etc.)

**Install Chronos:**

```bash
go get github.com/spawn08/chronos
```

**Key imports you'll use throughout this guide:**

```go
import (
    "context"
    "os"

    "github.com/spawn08/chronos/engine/graph"
    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/sdk/protocol"
    "github.com/spawn08/chronos/sdk/team"
)
```

---

## Core Concepts

### What Is an Agent?

An agent is a unit of work powered by an LLM. Each agent has a role, a system prompt that defines its behavior, and optionally, tools and capabilities.

**Lightweight agents** (model-only) need only a model — no graph, no storage, no database. This is the recommended way to build agents for team orchestration:

```go
researcher, _ := agent.New("researcher", "Researcher").
    Description("Researches topics and gathers facts").
    WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
    WithSystemPrompt("You are a research specialist. Given a topic, provide key facts.").
    AddCapability("research").
    Build()
```

**Graph-based agents** add durable execution with checkpointing, but are heavier — use them when you need workflows with multiple steps, interrupts, or persistence. See the [StateGraph guide](/guides/stategraph/) for details.

### What Is a Team?

A team is a group of agents that work together using a **strategy** — the pattern that decides who runs, in what order, and how results are combined.

```go
t := team.New("my-team", "My Team", team.StrategySequential)
t.AddAgent(researcher)
t.AddAgent(writer)

result, err := t.Run(ctx, graph.State{"message": "Write about renewable energy"})
```

### What Is graph.State?

`graph.State` is simply `map[string]any`. It carries data between agents. You put data in, agents read and write keys, and the final state is your result.

```go
input := graph.State{
    "message": "Explain quantum computing",
    "format":  "article",
}
```

### Four Strategies at a Glance

| Strategy | How It Works | Best For |
|----------|-------------|----------|
| **Sequential** | Agents run one after another in a pipeline | Content pipelines, multi-step processing |
| **Parallel** | All agents run at the same time, results merged | Independent analysis, multi-perspective work |
| **Router** | One agent is selected based on the input | Customer support, task classification |
| **Coordinator** | A supervisor agent decomposes the task and delegates | Complex projects, dynamic task planning |

---

## Creating Agents

Every team needs agents. Here's how to build them for each common role.

### Minimal Agent (Just a Model)

The simplest agent — a model with a system prompt:

```go
a, err := agent.New("helper", "Helper Agent").
    WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
    WithSystemPrompt("You are a helpful assistant.").
    Build()
if err != nil {
    log.Fatal(err)
}
```

### Agent with Capabilities

Capabilities are tags that tell routers and coordinators what an agent is good at:

```go
writer, _ := agent.New("writer", "Writer").
    Description("Writes polished content from research notes").
    WithModel(model.NewOpenAI(apiKey)).
    WithSystemPrompt("You are a writing specialist. Produce clear, engaging prose.").
    AddCapability("writing").
    AddCapability("editing").
    Build()
```

### Agent with Tools

Agents can call tools (functions) during execution:

```go
coder, _ := agent.New("coder", "Coder").
    WithModel(model.NewOpenAI(apiKey)).
    WithSystemPrompt("You are a Go programmer.").
    AddTool(&tool.Definition{
        Name:        "run_tests",
        Description: "Run the test suite",
        Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
        Permission:  tool.PermAllow,
        Handler: func(ctx context.Context, args map[string]any) (any, error) {
            return map[string]string{"status": "all tests passed"}, nil
        },
    }).
    Build()
```

### Using Different Providers

Each agent can use a different LLM provider:

```go
// Agent 1: OpenAI GPT-4o
agent1, _ := agent.New("gpt-agent", "GPT Agent").
    WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
    Build()

// Agent 2: Anthropic Claude
agent2, _ := agent.New("claude-agent", "Claude Agent").
    WithModel(model.NewAnthropic(os.Getenv("ANTHROPIC_API_KEY"))).
    Build()

// Agent 3: Local Ollama
agent3, _ := agent.New("local-agent", "Local Agent").
    WithModel(model.NewOllama("http://localhost:11434", "llama3.2")).
    Build()
```

---

## Strategy 1: Sequential (Pipeline)

Sequential runs agents one after another in a pipeline. Each agent receives the output of the previous agent and builds on it.

```
Input → [Agent A] → [Agent B] → [Agent C] → Result
```

### When to Use

- Multi-step content creation (research → write → review)
- Data processing pipelines (extract → transform → validate)
- Any workflow where each step depends on the previous step

### Basic Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/spawn08/chronos/engine/graph"
    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/sdk/team"
)

func main() {
    ctx := context.Background()
    apiKey := os.Getenv("OPENAI_API_KEY")

    // Step 1: Create specialist agents
    researcher, _ := agent.New("researcher", "Researcher").
        WithModel(model.NewOpenAI(apiKey)).
        WithSystemPrompt("You are a researcher. Given a topic, provide 3-5 key facts.").
        Build()

    writer, _ := agent.New("writer", "Writer").
        WithModel(model.NewOpenAI(apiKey)).
        WithSystemPrompt("You are a writer. Take the research provided and write a short article.").
        Build()

    reviewer, _ := agent.New("reviewer", "Reviewer").
        WithModel(model.NewOpenAI(apiKey)).
        WithSystemPrompt("You are an editor. Review the article for clarity and accuracy.").
        Build()

    // Step 2: Create a sequential team
    t := team.New("pipeline", "Content Pipeline", team.StrategySequential).
        AddAgent(researcher).
        AddAgent(writer).
        AddAgent(reviewer)

    // Step 3: Run the pipeline
    result, err := t.Run(ctx, graph.State{
        "message": "Write a short article about renewable energy trends",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Step 4: Read the final output
    fmt.Println(result["response"])
}
```

### How State Flows

The `"message"` key is the primary input. Each agent reads it, produces a `"response"`, and the response becomes available as `"_previous_response"` for the next agent:

| Step | Agent | Reads | Writes |
|------|-------|-------|--------|
| 1 | Researcher | `message` | `response` (research notes) |
| 2 | Writer | `message`, `_previous_response` (research) | `response` (article draft) |
| 3 | Reviewer | `message`, `_previous_response` (draft) | `response` (final article) |

The final `result["response"]` contains the reviewer's output.

### Viewing Communication History

Every team records all inter-agent messages. Use this for debugging or observability:

```go
for _, msg := range t.MessageHistory() {
    fmt.Printf("[%s] %s → %s: %s\n", msg.Type, msg.From, msg.To, msg.Subject)
}
```

---

## Strategy 2: Parallel (Fan-Out / Fan-In)

Parallel runs all agents at the same time on the same input, then merges their results into one output.

```
         ┌→ [Agent A] ─┐
Input ───┤→ [Agent B] ──┤──→ Merge → Result
         └→ [Agent C] ─┘
```

### When to Use

- Getting multiple perspectives on the same question
- Independent analyses that don't depend on each other
- Speeding up work by doing things simultaneously

### Basic Example

```go
ctx := context.Background()
apiKey := os.Getenv("OPENAI_API_KEY")

optimist, _ := agent.New("optimist", "Optimist").
    WithModel(model.NewOpenAI(apiKey)).
    WithSystemPrompt("Analyze the topic with an optimistic perspective. Focus on opportunities.").
    Build()

pessimist, _ := agent.New("pessimist", "Pessimist").
    WithModel(model.NewOpenAI(apiKey)).
    WithSystemPrompt("Analyze the topic with a critical perspective. Focus on risks.").
    Build()

realist, _ := agent.New("realist", "Realist").
    WithModel(model.NewOpenAI(apiKey)).
    WithSystemPrompt("Analyze the topic objectively. Balance opportunities and risks.").
    Build()

t := team.New("analysis", "Multi-Perspective Analysis", team.StrategyParallel).
    AddAgent(optimist).
    AddAgent(pessimist).
    AddAgent(realist)

result, err := t.Run(ctx, graph.State{
    "message": "What is the impact of AI on employment?",
})
if err != nil {
    log.Fatal(err)
}

// Default merge combines all "response" values separated by "---"
fmt.Println(result["response"])
```

### Controlling Concurrency

By default, all agents run simultaneously. Use `SetMaxConcurrency` to limit how many run at once — useful when you have many agents or limited API rate limits:

```go
t := team.New("large-team", "Large Team", team.StrategyParallel).
    AddAgent(agent1).
    AddAgent(agent2).
    AddAgent(agent3).
    AddAgent(agent4).
    AddAgent(agent5).
    SetMaxConcurrency(2) // only 2 agents run at a time
```

### Custom Merge Function

The default merge concatenates all responses. Use `SetMerge` for custom logic:

```go
t.SetMerge(func(results []graph.State) graph.State {
    merged := make(graph.State)
    for i, r := range results {
        // Namespace each agent's output to avoid key collisions
        key := fmt.Sprintf("perspective_%d", i+1)
        merged[key] = r["response"]
    }
    merged["total_perspectives"] = len(results)
    return merged
})
```

### Error Handling Strategies

Control what happens when an agent fails:

```go
// FailFast (default): Stop everything on first error
t.SetErrorStrategy(team.ErrorStrategyFailFast)

// Collect: Gather all errors, return them together
t.SetErrorStrategy(team.ErrorStrategyCollect)

// BestEffort: Ignore failures, return whatever succeeded
t.SetErrorStrategy(team.ErrorStrategyBestEffort)
```

**`ErrorStrategyFailFast`** cancels all running agents the moment one fails. Use this when every agent's result is critical.

**`ErrorStrategyCollect`** lets all agents finish, then returns a combined error listing every failure. Use this when you want a full picture of what went wrong.

**`ErrorStrategyBestEffort`** ignores failures and merges only the successful results. Use this when partial results are acceptable.

---

## Strategy 3: Router (Intelligent Dispatch)

Router examines the input and sends it to exactly one agent — the best one for the job.

```
         ┌→ [Agent A] (if condition A)
Input ───┤→ [Agent B] (if condition B)
         └→ [Agent C] (if condition C)
```

### When to Use

- Customer support routing (billing vs. technical vs. sales)
- Task classification and dispatch
- Any scenario where different inputs need different specialists

### Static Router (Function-Based)

The simplest router uses a function you write:

```go
ctx := context.Background()
apiKey := os.Getenv("OPENAI_API_KEY")

billing, _ := agent.New("billing", "Billing Agent").
    WithModel(model.NewOpenAI(apiKey)).
    WithSystemPrompt("You handle billing and payment questions.").
    AddCapability("billing").
    Build()

technical, _ := agent.New("technical", "Technical Agent").
    WithModel(model.NewOpenAI(apiKey)).
    WithSystemPrompt("You handle technical issues and troubleshooting.").
    AddCapability("technical").
    Build()

sales, _ := agent.New("sales", "Sales Agent").
    WithModel(model.NewOpenAI(apiKey)).
    WithSystemPrompt("You handle pricing, plans, and purchasing questions.").
    AddCapability("sales").
    Build()

t := team.New("support", "Customer Support", team.StrategyRouter).
    AddAgent(billing).
    AddAgent(technical).
    AddAgent(sales).
    SetRouter(func(state graph.State) string {
        msg, _ := state["message"].(string)
        lower := strings.ToLower(msg)
        switch {
        case strings.Contains(lower, "invoice") || strings.Contains(lower, "payment"):
            return "billing"
        case strings.Contains(lower, "error") || strings.Contains(lower, "crash"):
            return "technical"
        case strings.Contains(lower, "pricing") || strings.Contains(lower, "upgrade"):
            return "sales"
        default:
            return "technical" // default to technical support
        }
    })

// This will route to the billing agent
result, _ := t.Run(ctx, graph.State{"message": "I have a question about my invoice"})
fmt.Println(result["response"])
```

### Model-Based Router (LLM-Powered)

For more nuanced routing, let an LLM decide which agent should handle the input:

```go
router := model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

t := team.New("smart-support", "Smart Support", team.StrategyRouter).
    AddAgent(billing).
    AddAgent(technical).
    AddAgent(sales).
    SetModelRouter(func(ctx context.Context, state graph.State, agents []team.AgentInfo) (string, error) {
        msg, _ := state["message"].(string)

        // Build a prompt listing available agents
        prompt := fmt.Sprintf("Given this customer message:\n\"%s\"\n\nWhich agent should handle it? ", msg)
        prompt += "Available agents:\n"
        for _, a := range agents {
            prompt += fmt.Sprintf("- %s (ID: %s): %s\n", a.Name, a.ID, a.Description)
        }
        prompt += "\nRespond with ONLY the agent ID, nothing else."

        resp, err := router.Chat(ctx, &model.ChatRequest{
            Messages: []model.Message{{Role: "user", Content: prompt}},
        })
        if err != nil {
            return "", err
        }
        return strings.TrimSpace(resp.Content), nil
    })
```

### Capability-Based Routing (Automatic Fallback)

If you set neither `SetRouter` nor `SetModelRouter`, the router automatically scores agents based on how their advertised capabilities match the input state. This is a lightweight fallback that requires no configuration:

```go
// These agents advertise their capabilities
billing, _ := agent.New("billing", "Billing").
    AddCapability("billing").
    AddCapability("payment").
    Build()

technical, _ := agent.New("technical", "Technical").
    AddCapability("debugging").
    AddCapability("troubleshooting").
    Build()

t := team.New("auto-router", "Auto Router", team.StrategyRouter).
    AddAgent(billing).
    AddAgent(technical)

// If the state contains "billing" as a key or value, the billing agent wins
result, _ := t.Run(ctx, graph.State{
    "message":  "Help me with billing",
    "category": "billing",
})
```

---

## Strategy 4: Coordinator (LLM-Driven Supervisor)

Coordinator uses a supervisor agent that analyzes the task, creates an execution plan, and delegates sub-tasks to specialist agents. The coordinator can re-plan based on intermediate results.

```
Input → [Coordinator] → Plan → [Agent A] ─┐
                              → [Agent B] ──┤── Merge → (re-plan?) → Result
                              → [Agent C] ─┘
```

### When to Use

- Complex projects requiring task decomposition
- Dynamic workflows where the plan depends on intermediate results
- Any task where you'd assign a project manager to coordinate specialists

### Basic Example

```go
ctx := context.Background()
apiKey := os.Getenv("OPENAI_API_KEY")

// The supervisor agent — it creates the plan
supervisor, _ := agent.New("supervisor", "Project Manager").
    Description("Decomposes complex tasks and coordinates specialists").
    WithModel(model.NewOpenAI(apiKey)).
    WithSystemPrompt("You are a project coordinator.").
    Build()

// Specialist agents — they do the actual work
researcher, _ := agent.New("researcher", "Researcher").
    Description("Researches topics and provides factual analysis").
    WithModel(model.NewOpenAI(apiKey)).
    WithSystemPrompt("You are a research specialist. Provide thorough analysis.").
    Build()

writer, _ := agent.New("writer", "Writer").
    Description("Writes polished articles and reports").
    WithModel(model.NewOpenAI(apiKey)).
    WithSystemPrompt("You are a writing specialist. Produce clear, engaging content.").
    Build()

// Create the coordinator team
t := team.New("project", "Project Team", team.StrategyCoordinator).
    SetCoordinator(supervisor).
    AddAgent(researcher).
    AddAgent(writer).
    SetMaxIterations(2) // allow re-planning after first round

result, err := t.Run(ctx, graph.State{
    "message": "Create a report on electric vehicle adoption trends in Europe",
})
if err != nil {
    log.Fatal(err)
}
fmt.Println(result["response"])
```

### How the Coordinator Works

1. **Planning:** The coordinator LLM receives the task and a list of available agents (with their IDs, names, descriptions, and capabilities). It produces a JSON plan:

```json
{
  "tasks": [
    {"agent_id": "researcher", "description": "Research EV adoption data in Europe"},
    {"agent_id": "writer", "description": "Write a report from the research", "depends_on": "researcher"}
  ],
  "done": false
}
```

2. **Execution:** Tasks without `depends_on` run in parallel. Tasks with `depends_on` wait for their dependency to finish first. Each task is delegated through the bus.

3. **Re-planning (optional):** If `MaxIterations` > 1, the coordinator sees the results and decides whether to issue more tasks or mark the work as done (`"done": true`).

### Setting an Explicit Coordinator

Use `SetCoordinator` to designate the supervisor agent. This agent is **not** part of the worker pool — it only plans and delegates:

```go
t := team.New("team", "Team", team.StrategyCoordinator).
    SetCoordinator(supervisor).  // supervisor plans, does not execute tasks
    AddAgent(researcher).        // worker
    AddAgent(writer).            // worker
    AddAgent(reviewer)           // worker
```

### Without SetCoordinator (Backward Compatible)

If you don't call `SetCoordinator`, the **first agent** added via `AddAgent` acts as both coordinator and worker:

```go
t := team.New("team", "Team", team.StrategyCoordinator).
    AddAgent(leadAgent).    // first agent = coordinator
    AddAgent(worker1).
    AddAgent(worker2)
```

### Controlling Iterations

`SetMaxIterations` controls how many planning cycles the coordinator can perform:

```go
t.SetMaxIterations(3)  // up to 3 rounds of plan → execute → re-plan
```

Set to `1` (the default) for a single-shot plan without re-planning.

---

## Agent Communication

Agents in a team can communicate in three ways, listed from simplest to most flexible.

### 1. Shared State (Automatic)

When a team runs, state flows automatically between agents based on the strategy. You don't need to write any communication code — it just works.

```go
// Sequential: state flows from agent to agent
result, _ := t.Run(ctx, graph.State{"message": "Hello"})
// Every agent's output is merged into the result
```

### 2. Bus-Based Messaging (Structured)

The Protocol Bus routes typed messages between agents. It's built into every team.

**Delegate a task and wait for the result:**

```go
result, err := t.DelegateTask(ctx, "researcher", "writer", "draft-article",
    protocol.TaskPayload{
        Description: "Write a summary about solar energy",
        Input:       map[string]any{"message": "Write about solar energy"},
    })
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Success: %v\n", result.Success)
fmt.Printf("Output: %v\n", result.Output["response"])
```

**Ask a question and wait for an answer:**

```go
answer, err := t.Bus.Ask(ctx, "writer", "researcher", "What are the latest solar panel efficiency records?")
fmt.Println(answer)
```

**Broadcast an update to all agents:**

```go
err := t.Broadcast(ctx, "researcher", "status-update", map[string]any{
    "progress": 0.75,
    "message":  "Research phase 75% complete",
})
```

**Find agents by capability:**

```go
reviewers := t.Bus.FindByCapability("review")
for _, peer := range reviewers {
    fmt.Printf("Found reviewer: %s (%s)\n", peer.Name, peer.ID)
}
```

### 3. Direct Channels (Low-Latency Bypass)

For performance-critical paths, create a direct channel between two agents that bypasses the central bus entirely:

```go
// Create a direct channel with buffer size 128
dc := t.DirectChannel("researcher", "writer", 128)

// Send from researcher to writer (non-blocking if buffer has space)
go func() {
    body, _ := json.Marshal(map[string]string{
        "findings": "Solar efficiency reached 47.6% in 2025",
    })
    dc.AtoB <- &protocol.Envelope{
        Type:    protocol.TypeTaskResult,
        From:    "researcher",
        To:      "writer",
        Subject: "research_findings",
        Body:    body,
    }
}()

// Writer receives directly
msg := <-dc.AtoB
fmt.Println(string(msg.Body))

// Send from writer back to researcher
dc.BtoA <- &protocol.Envelope{...}
```

Direct channels are bidirectional: `AtoB` sends from the first agent to the second, `BtoA` sends in the reverse direction.

---

## Protocol Bus Reference

### Message Types

| Type | Constant | Purpose |
|------|----------|---------|
| Task Request | `protocol.TypeTaskRequest` | Ask an agent to perform work |
| Task Result | `protocol.TypeTaskResult` | Return the outcome of delegated work |
| Question | `protocol.TypeQuestion` | Ask an agent for information |
| Answer | `protocol.TypeAnswer` | Respond to a question |
| Broadcast | `protocol.TypeBroadcast` | Send an update to all agents |
| Handoff | `protocol.TypeHandoff` | Transfer ownership of a task/conversation |
| Status | `protocol.TypeStatus` | Report progress on long-running work |
| Ack | `protocol.TypeAck` | Acknowledge receipt of a message |
| Error | `protocol.TypeError` | Signal a failure |

### Priority Levels

```go
protocol.PriorityLow     // 0
protocol.PriorityNormal  // 1 (default)
protocol.PriorityHigh    // 2
protocol.PriorityUrgent  // 3
```

### Bus Configuration

Tune the bus for your workload:

```go
bus := protocol.NewBusWithConfig(protocol.BusConfig{
    InboxSize:  1024,  // per-agent inbox buffer (default: 512)
    HistoryCap: 8192,  // max retained history entries (default: 4096)
})
```

### Envelope Pooling

For high-throughput scenarios, use the envelope pool to reduce garbage collection pressure:

```go
env := protocol.AcquireEnvelope()
env.Type = protocol.TypeTaskRequest
env.From = "agent-a"
env.To = "agent-b"
env.Subject = "process-data"
env.Body, _ = json.Marshal(payload)

err := bus.Send(ctx, env)
protocol.ReleaseEnvelope(env) // return to pool when done
```

### Message History (Observability)

Every message sent through the bus is recorded for debugging:

```go
history := t.MessageHistory()
for _, msg := range history {
    fmt.Printf("[%s] %s → %s: %s (type: %s)\n",
        msg.CreatedAt.Format("15:04:05"),
        msg.From, msg.To,
        msg.Subject, msg.Type)
}
```

---

## Complete Example: Content Creation Pipeline

This runnable example shows a realistic multi-agent content pipeline using sequential strategy, with proper error handling:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/spawn08/chronos/engine/graph"
    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/sdk/team"
)

func main() {
    ctx := context.Background()
    apiKey := os.Getenv("OPENAI_API_KEY")
    if apiKey == "" {
        log.Fatal("Set OPENAI_API_KEY environment variable")
    }
    provider := model.NewOpenAI(apiKey)

    // Build specialist agents
    researcher, err := agent.New("researcher", "Researcher").
        Description("Gathers facts and data on a topic").
        WithModel(provider).
        WithSystemPrompt(`You are a research analyst.
When given a topic, provide 5 key facts with sources.
Format as a numbered list.`).
        AddCapability("research").
        Build()
    if err != nil {
        log.Fatalf("build researcher: %v", err)
    }

    writer, err := agent.New("writer", "Writer").
        Description("Writes articles from research notes").
        WithModel(provider).
        WithSystemPrompt(`You are a professional writer.
Take the research provided and write a clear, engaging article.
Use headers and short paragraphs. Target 300-500 words.`).
        AddCapability("writing").
        Build()
    if err != nil {
        log.Fatalf("build writer: %v", err)
    }

    reviewer, err := agent.New("reviewer", "Reviewer").
        Description("Reviews content for accuracy and quality").
        WithModel(provider).
        WithSystemPrompt(`You are a senior editor.
Review the article for factual accuracy, clarity, and grammar.
If the article is good, respond with the final version.
If changes are needed, make them and return the improved version.`).
        AddCapability("review").
        Build()
    if err != nil {
        log.Fatalf("build reviewer: %v", err)
    }

    // Assemble the team
    t := team.New("content-pipeline", "Content Pipeline", team.StrategySequential).
        AddAgent(researcher).
        AddAgent(writer).
        AddAgent(reviewer)

    // Run the pipeline
    result, err := t.Run(ctx, graph.State{
        "message": "Write a short article about the future of space tourism",
    })
    if err != nil {
        log.Fatalf("pipeline failed: %v", err)
    }

    fmt.Println("=== Final Article ===")
    fmt.Println(result["response"])
    fmt.Printf("\n=== Communication Log (%d messages) ===\n", len(t.MessageHistory()))
    for _, msg := range t.MessageHistory() {
        fmt.Printf("  %s → %s: %s\n", msg.From, msg.To, msg.Subject)
    }
}
```

## Complete Example: Smart Customer Support Router

This example routes customer messages to the right department using an LLM:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "strings"

    "github.com/spawn08/chronos/engine/graph"
    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/sdk/team"
)

func main() {
    ctx := context.Background()
    apiKey := os.Getenv("OPENAI_API_KEY")
    if apiKey == "" {
        log.Fatal("Set OPENAI_API_KEY environment variable")
    }
    provider := model.NewOpenAI(apiKey)

    billing, _ := agent.New("billing", "Billing Support").
        Description("Handles invoices, payments, refunds, and subscription changes").
        WithModel(provider).
        WithSystemPrompt("You are a billing support specialist. Help with payment and invoice questions.").
        AddCapability("billing").AddCapability("payments").
        Build()

    technical, _ := agent.New("technical", "Technical Support").
        Description("Handles bugs, errors, crashes, and technical troubleshooting").
        WithModel(provider).
        WithSystemPrompt("You are a technical support engineer. Help diagnose and fix issues.").
        AddCapability("debugging").AddCapability("troubleshooting").
        Build()

    sales, _ := agent.New("sales", "Sales").
        Description("Handles pricing questions, plan upgrades, and new purchases").
        WithModel(provider).
        WithSystemPrompt("You are a sales representative. Help with pricing and purchasing decisions.").
        AddCapability("pricing").AddCapability("sales").
        Build()

    // Model-based routing — the LLM picks the best agent
    routerModel := model.NewOpenAI(apiKey)

    t := team.New("support", "Customer Support", team.StrategyRouter).
        AddAgent(billing).
        AddAgent(technical).
        AddAgent(sales).
        SetModelRouter(func(ctx context.Context, state graph.State, agents []team.AgentInfo) (string, error) {
            msg, _ := state["message"].(string)
            prompt := fmt.Sprintf(
                "Customer message: \"%s\"\n\nAvailable agents:\n", msg)
            for _, a := range agents {
                prompt += fmt.Sprintf("- ID=%s: %s — %s\n", a.ID, a.Name, a.Description)
            }
            prompt += "\nRespond with ONLY the agent ID that should handle this message."

            resp, err := routerModel.Chat(ctx, &model.ChatRequest{
                Messages: []model.Message{{Role: "user", Content: prompt}},
            })
            if err != nil {
                return "", err
            }
            return strings.TrimSpace(resp.Content), nil
        })

    // Test with different customer messages
    messages := []string{
        "My invoice shows a double charge for last month",
        "The app crashes when I try to upload a file larger than 10MB",
        "What's the price difference between Pro and Enterprise plans?",
    }

    for _, msg := range messages {
        result, err := t.Run(ctx, graph.State{"message": msg})
        if err != nil {
            log.Printf("Error: %v", err)
            continue
        }
        fmt.Printf("Customer: %s\n", msg)
        fmt.Printf("Response: %s\n\n", result["response"])
    }
}
```

---

## Team Builder Reference

### Constructor

```go
team.New(id string, name string, strategy team.Strategy) *Team
```

### Configuration Methods

All methods return `*Team` for chaining.

| Method | Description | Strategies |
|--------|-------------|------------|
| `AddAgent(a *agent.Agent)` | Add an agent to the team | All |
| `SetRouter(fn RouterFunc)` | Set a static routing function | Router |
| `SetModelRouter(fn ModelRouterFunc)` | Set an LLM-based routing function | Router |
| `SetMerge(fn MergeFunc)` | Set a custom result merge function | Parallel |
| `SetMaxConcurrency(n int)` | Limit concurrent goroutines | Parallel |
| `SetErrorStrategy(es ErrorStrategy)` | Control failure behavior | Parallel |
| `SetCoordinator(a *agent.Agent)` | Set the supervisor agent | Coordinator |
| `SetMaxIterations(n int)` | Max planning iterations | Coordinator |

### Execution

```go
result, err := t.Run(ctx, graph.State{"message": "your task"})
```

Returns a `graph.State` (which is `map[string]any`) containing the combined output from all agents that ran.

### Communication

| Method | Description |
|--------|-------------|
| `DelegateTask(ctx, from, to, subject, payload)` | Send a task and wait for the result |
| `Broadcast(ctx, from, subject, data)` | Send a message to all agents |
| `DirectChannel(agentA, agentB, bufSize)` | Create a direct channel between two agents |
| `MessageHistory()` | Get all messages exchanged during the run |
| `Bus` | Access the underlying Protocol Bus directly |

### Type Signatures

```go
type RouterFunc func(state graph.State) string

type ModelRouterFunc func(ctx context.Context, state graph.State, agents []AgentInfo) (string, error)

type MergeFunc func(results []graph.State) graph.State

type AgentInfo struct {
    ID           string   `json:"id"`
    Name         string   `json:"name"`
    Description  string   `json:"description"`
    Capabilities []string `json:"capabilities"`
}
```

---

## Running the Demo

Chronos includes a complete demo that exercises all four strategies, direct channels, and bus delegation:

```bash
# With a real provider
OPENAI_API_KEY=sk-... go run ./examples/multi_agent/

# Without an API key (uses a mock provider for demonstration)
go run ./examples/multi_agent/
```

The demo shows:
1. Sequential pipeline (Researcher → Writer → Reviewer)
2. Parallel fan-out with bounded concurrency
3. Router with static dispatch
4. Coordinator with LLM-driven planning
5. Direct agent-to-agent channel
6. Bus-based task delegation

---

## Tips and Best Practices

**Start with lightweight agents.** Use model-only agents (`agent.New(...).WithModel(...).Build()`) for team orchestration. Only add graphs and storage when you need durable workflows.

**Give agents clear descriptions and capabilities.** These are used by the coordinator for planning and by the router for automatic dispatch.

**Use system prompts to define boundaries.** Tell each agent what it should and shouldn't do. Be specific: "You are a researcher. Provide facts, not opinions."

**Choose the right error strategy for parallel teams.** Use `FailFast` for critical pipelines, `BestEffort` when partial results are acceptable.

**Use `SetMaxConcurrency` with parallel teams.** If you have many agents or limited API rate limits, bound the concurrency to avoid hitting rate limits.

**Use `SetMaxIterations` wisely with coordinator.** Each iteration costs a model call for re-planning. Start with 1 and increase only if you need iterative refinement.

**Use direct channels for hot paths.** When two specific agents exchange many messages, a direct channel avoids the overhead of bus routing.
