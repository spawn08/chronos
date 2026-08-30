---
name: chronos-teams
description: Build multi-agent teams with Chronos — sequential pipelines, parallel fan-out, routers, coordinators, and swarms. Each topology has ready-to-copy YAML in examples/.
---

# Chronos Multi-Agent Teams

## Activation
Use this skill when:
- Building multi-agent systems or teams
- Developer asks about agent orchestration, routing, or handoffs
- Creating pipelines, fan-out, triage, delegation, or swarm patterns

## Topology Selection

| Topology | Directory | Flow | When to use |
|----------|-----------|------|-------------|
| **Pipeline** | `examples/pipeline/` | A → B → C | Sequential stages (research → analyze → write) |
| **Router** | `examples/router/` | Router → best agent | Request triage to specialists |
| **Coordinator** | `examples/coordinator/` | Boss → workers → Boss | Complex tasks needing delegation |
| **Swarm** | `examples/swarm/` | A ↔ B ↔ C | Dynamic conversational handoffs |

Also available: `parallel` (fan-out + merge) and `hierarchy` (tree of coordinators).

---

## Topology: Pipeline (Sequential)

**Files:** `examples/pipeline/agents.yaml`

Each agent receives the previous agent's output. Order matters.

```yaml
defaults:
  model:
    provider: anthropic
    model: claude-sonnet-4-20250514
    api_key: ${ANTHROPIC_API_KEY}
  storage: { backend: sqlite, dsn: "pipeline.db" }

agents:
  - id: "researcher"
    name: "Researcher"
    description: "Gathers information on the topic"
    system_prompt: |
      Research the given topic thoroughly.
      Output structured findings with sources.

  - id: "analyst"
    name: "Analyst"
    description: "Analyzes research findings"
    system_prompt: |
      Analyze the research findings.
      Identify key themes, patterns, and insights.

  - id: "writer"
    name: "Writer"
    description: "Writes polished final output"
    system_prompt: |
      Write a clear, well-structured report from the analysis.
      Include an executive summary, key findings, and recommendations.

teams:
  - id: "research-pipeline"
    name: "Research Pipeline"
    strategy: "sequential"
    agents: ["researcher", "analyst", "writer"]
    max_iterations: 10
    error_strategy: "fail_fast"
```

Run: `chronos team run -c agents.yaml research-pipeline "AI trends in 2025"` (team id and message are both positional; there is no `-t`/`-m` flag)

---

## Topology: Router (Triage)

**Files:** `examples/router/agents.yaml`

A router selects the best agent per request. Two routing modes:
- `model` — an LLM reads agent descriptions and picks
- `capability` — matches agent `capabilities` to the request

```yaml
defaults:
  model:
    provider: anthropic
    model: claude-sonnet-4-20250514
    api_key: ${ANTHROPIC_API_KEY}
  storage: { backend: sqlite, dsn: "router.db" }

agents:
  - id: "billing"
    name: "Billing Agent"
    description: "Handles billing, payments, refunds, and invoicing"
    capabilities: ["billing", "payments", "refunds"]
    system_prompt: "You are a billing specialist."

  - id: "technical"
    name: "Technical Agent"
    description: "Handles technical issues, bugs, and troubleshooting"
    capabilities: ["debugging", "troubleshooting", "technical"]
    system_prompt: "You are a technical support engineer."

  - id: "sales"
    name: "Sales Agent"
    description: "Handles pricing, plans, and upgrade inquiries"
    capabilities: ["pricing", "plans", "upgrades"]
    system_prompt: "You are a sales specialist."

teams:
  - id: "support-router"
    name: "Support Router"
    strategy: "router"
    router: "model"                 # or "capability"
    router_model:                   # fast model for routing decisions
      provider: anthropic
      model: claude-haiku-4-5-20251001
      api_key: ${ANTHROPIC_API_KEY}
    agents: ["billing", "technical", "sales"]
    error_strategy: "fail_fast"
```

---

## Topology: Coordinator (Boss/Worker)

**Files:** `examples/coordinator/agents.yaml`

A coordinator agent delegates tasks to workers and synthesizes outputs.

```yaml
defaults:
  model:
    provider: anthropic
    model: claude-sonnet-4-20250514
    api_key: ${ANTHROPIC_API_KEY}
  storage: { backend: sqlite, dsn: "coordinator.db" }

agents:
  - id: "manager"
    name: "Project Manager"
    description: "Breaks tasks into subtasks and delegates to workers"
    system_prompt: |
      You are a project manager. Given a task:
      1. Break it into subtasks
      2. Delegate each to the best worker (dev, designer, reviewer)
      3. Synthesize worker outputs into a cohesive final result
      Workers available: developer, designer, reviewer.

  - id: "developer"
    name: "Developer"
    description: "Implements code and technical solutions"
    system_prompt: "You implement code. Output working code with explanations."

  - id: "designer"
    name: "Designer"
    description: "Creates UI/UX designs and mockups"
    system_prompt: "You design user interfaces. Output design specifications."

  - id: "reviewer"
    name: "Reviewer"
    description: "Reviews work for quality and correctness"
    system_prompt: "You review code and designs. Output specific, actionable feedback."

teams:
  - id: "dev-team"
    name: "Development Team"
    strategy: "coordinator"
    coordinator: "manager"
    agents: ["manager", "developer", "designer", "reviewer"]
    max_iterations: 15
    error_strategy: "collect"
```

---

## Topology: Swarm (Dynamic Handoff)

**Files:** `examples/swarm/agents.yaml`

Agents hand off to each other dynamically during a conversation. Inspired by OpenAI Swarm.

```yaml
defaults:
  model:
    provider: anthropic
    model: claude-sonnet-4-20250514
    api_key: ${ANTHROPIC_API_KEY}
  storage: { backend: sqlite, dsn: "swarm.db" }

agents:
  - id: "greeter"
    name: "Greeter"
    description: "Greets customers, identifies intent, hands off"
    system_prompt: |
      Greet the customer. Determine their need:
      - Billing question → hand off to billing
      - Technical issue → hand off to technical
      - Just chatting → handle yourself

  - id: "billing"
    name: "Billing"
    description: "Handles billing and payment queries"
    system_prompt: "Handle billing. Hand off to technical if it's a tech issue."

  - id: "technical"
    name: "Technical"
    description: "Handles technical support"
    system_prompt: "Handle technical issues. Escalate to escalation if unresolvable."

  - id: "escalation"
    name: "Escalation"
    description: "Handles complex escalated issues"
    system_prompt: "Handle escalated issues with extra care and detail."

teams:
  - id: "support-swarm"
    name: "Support Swarm"
    strategy: "swarm"
    initial_agent: "greeter"
    max_handoffs: 10
    agents: ["greeter", "billing", "technical", "escalation"]
    error_strategy: "fail_fast"
```

---

## Go API Reference

```go
import "github.com/spawn08/chronos/sdk/team"

// Strategies
team.StrategySequential   // sequential
team.StrategyParallel     // parallel
team.StrategyRouter       // router
team.StrategyCoordinator  // coordinator
team.StrategySwarm        // swarm
team.StrategyHierarchy    // hierarchy

// Error strategies
team.ErrorStrategyFailFast    // stop on first error
team.ErrorStrategyCollect     // collect errors, continue
team.ErrorStrategyBestEffort  // ignore errors, partial results

// Build programmatically
t := team.New("id", "name", team.StrategySequential)
t.AddAgent(agentA)
t.AddAgent(agentB)
t.SetMaxIterations(10)
t.SetMaxConcurrency(3)          // parallel only
t.SetCoordinator(bossAgent)     // coordinator only
t.SetErrorStrategy(team.ErrorStrategyFailFast)

// Router functions
t.SetRouter(func(ctx, state) (*agent.Agent, error) { ... })
t.SetModelRouter(func(ctx, input, agents) (*agent.Agent, error) { ... })

// Merge function (parallel)
t.SetMerge(func(results []graph.State) graph.State { ... })

// Execute
result, err := t.Run(ctx, graph.State{"input": "task"})

// Communication
team.DelegateTask(ctx, from, to, "subject", "task")
team.Broadcast(ctx, from, "subject", data)
team.DirectChannel(agentA, agentB, bufSize)
```

### CLI
```bash
chronos team run -c agents.yaml <team-id> "task"       # team id and message are positional
chronos --json team run -c agents.yaml <team-id> "task"  # one JSON object on stdout
chronos team list -c agents.yaml
```
