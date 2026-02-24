---
title: "YAML Agent Examples"
permalink: /guides/yaml-examples/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
---

This guide shows you how to define real-world AI agents and teams in YAML and run them from the Chronos CLI. No Go code is required — everything runs from the command line.

---

## How It Works

A Chronos YAML config file defines **agents** (individual AI workers) and **teams** (groups of agents that collaborate). The CLI reads the file, connects to the LLM providers, and runs everything.

```yaml
# Agents: individual AI workers with their own system prompts
agents:
  - id: researcher
    name: Research Analyst
    model:
      provider: openai
      model: gpt-4o
      api_key: ${OPENAI_API_KEY}
    system_prompt: You are a research analyst.

  - id: writer
    name: Content Writer
    model:
      provider: openai
      model: gpt-4o
      api_key: ${OPENAI_API_KEY}
    system_prompt: You are a content writer.

# Teams: agents working together with a strategy
teams:
  - id: pipeline
    name: Content Pipeline
    strategy: sequential        # researcher runs first, then writer
    agents: [researcher, writer]
```

**Run it:**

```bash
# Chat with an individual agent
chronos agent chat researcher

# Run a team on a task
chronos team run pipeline "Write about electric vehicles"
```

---

## Prerequisites

1. **Go 1.24+** installed
2. **Chronos cloned:**

```bash
git clone https://github.com/spawn08/chronos.git
cd chronos
```

3. **At least one API key.** Set it as an environment variable:

```bash
export OPENAI_API_KEY=sk-your-key-here
```

4. **Verify the build:**

```bash
go build ./...
```

---

## YAML Config Structure

Every config file has three optional sections:

```yaml
# 1. Defaults — shared settings inherited by all agents
defaults:
  model:
    provider: openai
    api_key: ${OPENAI_API_KEY}
  storage:
    backend: none

# 2. Agents — individual AI workers
agents:
  - id: my-agent
    name: My Agent
    system_prompt: You are a helpful assistant.

# 3. Teams — groups of agents with a strategy
teams:
  - id: my-team
    name: My Team
    strategy: sequential
    agents: [agent-1, agent-2]
```

All string values support `${ENV_VAR}` expansion. See the [Configuration Reference](/getting-started/configuration/) for the full field list.

---

## Example 1: Single Agent (Simplest Possible)

One agent, one provider, one system prompt.

### Create `.chronos/agents.yaml`

```yaml
agents:
  - id: assistant
    name: Personal Assistant
    model:
      provider: openai
      model: gpt-4o
      api_key: ${OPENAI_API_KEY}
    storage:
      backend: none
    system_prompt: |
      You are a friendly personal assistant.
      Be concise and helpful. Use bullet points for lists.
```

### Run it

```bash
# Set your API key
export OPENAI_API_KEY=sk-your-key-here

# List agents to verify config loaded
go run ./cli/main.go agent list

# Send a one-shot message
go run ./cli/main.go run "What are three interesting facts about the moon?"

# Start interactive chat
go run ./cli/main.go repl
```

Since the file is at `.chronos/agents.yaml`, the CLI discovers it automatically. No `CHRONOS_CONFIG` needed.

---

## Example 2: Customer Support Router

Three specialist agents handle different types of customer inquiries. The router team automatically dispatches messages to the agent whose capabilities best match the query.

### Create `customer-support.yaml`

```yaml
defaults:
  model:
    provider: openai
    api_key: ${OPENAI_API_KEY}
    model: gpt-4o
  storage:
    backend: none

agents:
  - id: billing-support
    name: Billing Support Agent
    description: Handles invoices, payments, refunds, and subscription changes
    system_prompt: |
      You are a billing support specialist at a SaaS company.

      Your responsibilities:
      - Answer questions about invoices and billing cycles
      - Process refund requests (ask for order ID and reason)
      - Explain pricing tiers and subscription changes

      Always be polite. Ask for the customer's account ID first.
    capabilities:
      - billing
      - payments
      - refunds

  - id: technical-support
    name: Technical Support Agent
    description: Diagnoses bugs, errors, and technical issues
    system_prompt: |
      You are a senior technical support engineer.

      Your approach:
      1. Ask clarifying questions about the issue
      2. Check for common causes
      3. Provide step-by-step troubleshooting
      4. If unresolved, suggest filing a bug report

      Always ask for: error messages, steps to reproduce, OS version.
    capabilities:
      - debugging
      - troubleshooting

  - id: sales-support
    name: Sales Agent
    description: Handles pricing questions, demos, and plan upgrades
    system_prompt: |
      You are a friendly sales representative.

      Pricing:
      - Starter: $29/month (5 users, 10GB)
      - Pro: $99/month (25 users, 100GB)
      - Enterprise: Custom pricing (unlimited)

      Focus on understanding the customer's needs before recommending a plan.
    capabilities:
      - sales
      - pricing

teams:
  - id: support
    name: Customer Support Router
    strategy: router
    agents:
      - billing-support
      - technical-support
      - sales-support
```

### Run it

```bash
export OPENAI_API_KEY=sk-your-key-here

# Point the CLI to this config file
export CHRONOS_CONFIG=customer-support.yaml

# See all agents and teams
go run ./cli/main.go agent list
go run ./cli/main.go team list

# Run the router team — it picks the right agent automatically
go run ./cli/main.go team run support "I was charged twice on my last invoice"
go run ./cli/main.go team run support "The app crashes when I export a PDF"
go run ./cli/main.go team run support "What's the difference between Pro and Enterprise?"

# Or chat directly with a specific agent
go run ./cli/main.go agent chat billing-support
```

### How routing works

The router matches the message against each agent's `capabilities` and `description`. When the customer says "charged twice", the router picks `billing-support` because its capabilities include "payments" and "refunds".

---

## Example 3: Content Creation Pipeline

Three agents work as a sequential pipeline: a researcher gathers facts, a writer crafts an article, and an editor polishes it. Each agent receives the previous agent's output as context.

### Create `content-pipeline.yaml`

```yaml
defaults:
  model:
    provider: openai
    api_key: ${OPENAI_API_KEY}
    model: gpt-4o
  storage:
    backend: none

agents:
  - id: researcher
    name: Research Analyst
    description: Researches topics and provides factual analysis
    system_prompt: |
      You are a research analyst.
      Given a topic, provide 5 key facts with specific numbers or data.
      Format as a numbered list. Be factual — no opinions, just data.
    capabilities:
      - research

  - id: writer
    name: Content Writer
    description: Writes articles from research notes
    system_prompt: |
      You are a professional writer.
      Given research notes, write a 300-500 word article with:
      - An engaging opening
      - Clear headers
      - A forward-looking conclusion
      Do NOT invent facts. Use only the provided research.
    capabilities:
      - writing

  - id: editor
    name: Senior Editor
    description: Reviews and improves content
    system_prompt: |
      You are a senior editor.
      Review the article and improve it:
      - Fix grammar and spelling
      - Improve flow and readability
      - Tighten wordy sections
      Return the final, polished version.
    capabilities:
      - editing

teams:
  - id: pipeline
    name: Content Pipeline
    strategy: sequential
    agents:
      - researcher
      - writer
      - editor
```

### Run it

```bash
export OPENAI_API_KEY=sk-your-key-here
export CHRONOS_CONFIG=content-pipeline.yaml

# See the team configuration
go run ./cli/main.go team show pipeline

# Run the full pipeline
go run ./cli/main.go team run pipeline "Write a short article about the rise of electric vehicles"
```

### How the pipeline flows

```
"Write about EVs" ──→ [Researcher] ──→ [Writer] ──→ [Editor] ──→ Final Article
                       5 key facts      300-word      Polished
                       with data        article       final draft
```

Each agent sees the previous agent's response:
- The **Writer** receives the Researcher's facts and writes from them
- The **Editor** receives the Writer's draft and refines it

---

## Example 4: Software Development Team (Coordinator)

A tech lead decomposes a feature request into sub-tasks and delegates to specialist developers. The coordinator strategy uses the LLM to plan, assign tasks, and iterate.

### Create `coding-team.yaml`

```yaml
defaults:
  model:
    provider: openai
    api_key: ${OPENAI_API_KEY}
    model: gpt-4o
  storage:
    backend: none

agents:
  - id: tech-lead
    name: Technical Lead
    description: Plans architecture and coordinates the development team
    system_prompt: |
      You are a senior technical lead. When given a feature request:
      1. Break it into clear, actionable sub-tasks
      2. Identify the right specialist for each task
      3. Specify the order of operations
      Be specific with your task descriptions.
    capabilities:
      - architecture
      - planning

  - id: backend-dev
    name: Backend Developer
    description: Implements server-side code and APIs
    system_prompt: |
      You are an expert backend developer. Write clean, idiomatic Go code.
      Include input validation, error handling, and tests.
    capabilities:
      - backend
      - golang

  - id: frontend-dev
    name: Frontend Developer
    description: Implements user interfaces
    system_prompt: |
      You are a senior frontend developer. Write TypeScript/React code.
      Focus on accessibility, error handling, and responsive design.
    capabilities:
      - frontend
      - react

  - id: code-reviewer
    name: Code Reviewer
    description: Reviews code for bugs and best practices
    system_prompt: |
      You are a code reviewer. Check for:
      1. Correctness
      2. Security issues
      3. Performance problems
      4. Code quality
    capabilities:
      - code-review
      - security

teams:
  - id: dev-team
    name: Development Team
    strategy: coordinator
    coordinator: tech-lead
    agents:
      - backend-dev
      - frontend-dev
      - code-reviewer
    max_iterations: 2
```

### Run it

```bash
export OPENAI_API_KEY=sk-your-key-here
export CHRONOS_CONFIG=coding-team.yaml

# Inspect the team
go run ./cli/main.go team show dev-team

# Run a feature request
go run ./cli/main.go team run dev-team "Build a user registration feature with email/password signup and a registration form"
```

### How the coordinator works

```
Feature Request ──→ [Tech Lead] ──→ Plan:
                                    ├── Task 1: backend-dev → "Build signup API"
                                    ├── Task 2: frontend-dev → "Build form" (depends on Task 1)
                                    └── Task 3: code-reviewer → "Review code" (depends on Task 2)

                    [Tech Lead] ──→ Reviews results → Done ✓ (or re-plans)
```

The `max_iterations: 2` allows the tech lead to review results and re-plan once if needed.

---

## Example 5: Multi-Provider Parallel Comparison

Different LLM providers answer the same question in parallel, so you can compare outputs.

### Create `multi-provider.yaml`

```yaml
agents:
  - id: openai-agent
    name: GPT-4o Agent
    model:
      provider: openai
      model: gpt-4o
      api_key: ${OPENAI_API_KEY}
    storage:
      backend: none
    system_prompt: You are a helpful assistant powered by GPT-4o.

  - id: claude-agent
    name: Claude Agent
    model:
      provider: anthropic
      model: claude-sonnet-4-6
      api_key: ${ANTHROPIC_API_KEY}
    storage:
      backend: none
    system_prompt: You are Claude, an AI assistant by Anthropic.

  - id: local-agent
    name: Local Agent
    model:
      provider: ollama
      model: llama3.2
      base_url: http://localhost:11434
    storage:
      backend: none
    system_prompt: You are a local AI assistant. All data stays private.

teams:
  - id: compare
    name: Provider Comparison
    strategy: parallel
    agents:
      - openai-agent
      - claude-agent
    max_concurrency: 3
    error_strategy: best_effort
```

### Run it

```bash
export OPENAI_API_KEY=sk-your-key-here
export ANTHROPIC_API_KEY=sk-ant-your-key-here
export CHRONOS_CONFIG=multi-provider.yaml

# Chat with individual providers
go run ./cli/main.go agent chat openai-agent
go run ./cli/main.go agent chat claude-agent

# Run both in parallel on the same question
go run ./cli/main.go team run compare "Explain quantum entanglement in 2 sentences"

# For the local agent, make sure Ollama is running:
# ollama serve && ollama pull llama3.2
go run ./cli/main.go agent chat local-agent
```

The `error_strategy: best_effort` means if one provider fails (e.g., missing API key), the other results are still returned.

---

## Team Strategies Reference

| Strategy | How it works | Best for |
|----------|-------------|----------|
| `sequential` | Agents run in order; each sees the previous agent's output | Pipelines: research → write → edit |
| `parallel` | Agents run concurrently on the same input | Getting multiple perspectives, comparisons |
| `router` | One agent is selected based on capabilities matching | Customer support, intent-based dispatch |
| `coordinator` | A supervisor LLM plans and delegates sub-tasks | Complex projects needing decomposition |

### Team config fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique team identifier (used in `team run`) |
| `name` | string | Display name |
| `strategy` | string | `sequential`, `parallel`, `router`, or `coordinator` |
| `agents` | list | Agent IDs (order matters for sequential) |
| `coordinator` | string | Agent ID for the coordinator strategy |
| `max_concurrency` | int | Max parallel goroutines (parallel strategy) |
| `max_iterations` | int | Max coordinator planning loops |
| `error_strategy` | string | `fail_fast`, `collect`, or `best_effort` |

---

## CLI Commands Reference

### Agent commands

```bash
# List all agents from your config
go run ./cli/main.go agent list

# Show details for a specific agent
go run ./cli/main.go agent show <agent-id>

# Start interactive chat with an agent
go run ./cli/main.go agent chat <agent-id>

# One-shot message to the default (first) agent
go run ./cli/main.go run "your message here"

# One-shot message to a specific agent
go run ./cli/main.go run --agent <agent-id> "your message here"
```

### Team commands

```bash
# List all teams
go run ./cli/main.go team list

# Show team configuration
go run ./cli/main.go team show <team-id>

# Run a team on a task
go run ./cli/main.go team run <team-id> "your task description"
```

### Specifying a config file

By default, the CLI looks for `.chronos/agents.yaml` in the current directory. To use a different file:

```bash
# Option 1: Environment variable
export CHRONOS_CONFIG=/path/to/your-config.yaml
go run ./cli/main.go team run my-team "do something"

# Option 2: Inline for a single command
CHRONOS_CONFIG=my-config.yaml go run ./cli/main.go team run my-team "do something"
```

---

## Running the Bundled Examples

The repository includes ready-to-run YAML configs in `examples/yaml-configs/`:

```bash
export OPENAI_API_KEY=sk-your-key-here

# Customer support — router dispatches to billing/technical/sales
CHRONOS_CONFIG=examples/yaml-configs/customer-support.yaml \
  go run ./cli/main.go team run support "I need a refund for order #12345"

# Content pipeline — sequential research → write → edit
CHRONOS_CONFIG=examples/yaml-configs/content-pipeline.yaml \
  go run ./cli/main.go team run pipeline "Write about renewable energy trends"

# Coding team — coordinator delegates to backend/frontend/reviewer
CHRONOS_CONFIG=examples/yaml-configs/coding-team.yaml \
  go run ./cli/main.go team run dev-team "Build a REST API for user management"

# Multi-provider — parallel comparison of different LLMs
CHRONOS_CONFIG=examples/yaml-configs/multi-provider.yaml \
  go run ./cli/main.go team run compare "What is the meaning of life?"
```

---

## Tips

- **Use `storage: backend: none` for team agents.** Agents in teams don't need their own database. This avoids creating unnecessary SQLite files.

- **Write detailed `description` fields.** The coordinator and router strategies use descriptions to decide which agent handles each task. Vague descriptions lead to poor results.

- **Use `capabilities` tags.** The router scores agents based on these tags against the input message. Be specific: `"api-design"` is better than `"development"`.

- **One config per use case.** Keep separate YAML files for different workflows rather than putting everything in one file.

- **Use `defaults` to avoid repetition.** Put your provider, API key, and storage settings in `defaults`. Individual agents only need to override what's different.
