---
title: "YAML Agent Examples"
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
      model: gpt-5.5
      api_key: ${OPENAI_API_KEY}
    system_prompt: You are a research analyst.

  - id: writer
    name: Content Writer
    model:
      provider: openai
      model: gpt-5.5
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
# Point the CLI at your config with -c (the file can be named anything).
# Chat with an individual agent
chronos -c my-agents.yaml agent chat researcher

# Run a team on a task
chronos -c my-agents.yaml team run pipeline "Write about electric vehicles"
```

:::info How the CLI finds your config
`pipeline` and `researcher` above are **ids defined inside the file** — not filenames.
The CLI locates the file in this order: **`-c <file>`** (or `--config`) → **`CHRONOS_CONFIG`** env var
→ **`.chronos/agents.yaml`** in the current directory → **`~/.chronos/agents.yaml`**.
So `chronos team show pipeline` only works if the file is at one of the default paths;
otherwise pass it explicitly: `chronos -c content-pipeline.yaml team show pipeline`.
:::

---

## Prerequisites

1. **Install the `chronos` CLI.** The quickest way is the install script (see the
   [CLI Install](/getting-started/cli-install/) guide for all options):

   ```bash
   curl -fsSL https://raw.githubusercontent.com/spawn08/chronos/main/install.sh | bash
   ```

   Or build it from source (requires Go 1.24+):

   ```bash
   git clone https://github.com/spawn08/chronos.git
   cd chronos
   go build -o chronos ./cli && sudo mv chronos /usr/local/bin/
   ```

2. **Verify it's on your PATH:**

   ```bash
   chronos version
   ```

3. **At least one API key.** Set it as an environment variable:

   ```bash
   export OPENAI_API_KEY=sk-your-key-here
   ```

:::note Running from source without installing
Prefer not to install a binary? Anywhere this guide says `chronos`, you can
substitute `go run ./cli/main.go` from a cloned repo — e.g.
`go run ./cli/main.go agent list`.
:::

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
      model: gpt-5.5
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
chronos agent list

# Send a one-shot message
chronos run "What are three interesting facts about the moon?"

# Start interactive chat
chronos repl
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
    model: gpt-5.5
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
    router: model              # LLM reads each agent's description and picks one (default)
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
chronos agent list
chronos team list

# Run the router team — it picks the right agent automatically
chronos team run support "I was charged twice on my last invoice"
chronos team run support "The app crashes when I export a PDF"
chronos team run support "What's the difference between Pro and Enterprise?"

# Or chat directly with a specific agent
chronos agent chat billing-support
```

### How routing works

The router picks **exactly one** agent to handle each message. Two modes are available via the `router` field:

- **`model`** (default) — an LLM reads every agent's `name`, `description`, and `capabilities` and reasons about which one best fits the request. When the customer says "charged twice", it selects `billing-support`. This is what makes routing understand intent rather than keywords.
- **`capability`** — a zero-LLM heuristic that scores agents by whether their `capabilities` appear as keys/values in the run state. Fast and free, but it does **not** read the message text; use it only when your caller sets capability keys in state explicitly.

By default the router reuses the **first member agent's model** for the routing decision. To route with a cheaper/faster model than your workers, add a `router_model` block:

```yaml
teams:
  - id: support
    name: Customer Support Router
    strategy: router
    router: model
    router_model:
      provider: openai
      model: gpt-4o-mini        # cheap model just for the dispatch decision
      api_key: ${OPENAI_API_KEY}
    agents:
      - billing-support
      - technical-support
      - sales-support
```

:::note
A router dispatches to a **single** agent. For a research → write → edit flow where every agent runs in turn, use `strategy: sequential` instead.
:::

---

## Example 3: Content Creation Pipeline

Three agents work as a sequential pipeline: a researcher gathers facts, a writer crafts an article, and an editor polishes it. Each agent receives the previous agent's output as context.

### Create `content-pipeline.yaml`

```yaml
defaults:
  model:
    provider: openai
    api_key: ${OPENAI_API_KEY}
    model: gpt-5.5
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
chronos team show pipeline

# Run the full pipeline
chronos team run pipeline "Write a short article about the rise of electric vehicles"
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
    model: gpt-5.5
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
chronos team show dev-team

# Run a feature request
chronos team run dev-team "Build a user registration feature with email/password signup and a registration form"
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

Each agent uses a different provider — OpenAI, Anthropic, Gemini, Azure OpenAI, and
Grok (xAI) — and the `compare` team fans one prompt out to all of them in parallel.
See [Provider Configurations](#provider-configurations) for the full per-provider reference.

```yaml
agents:
  - id: openai-agent
    name: GPT Agent
    model:
      provider: openai
      model: gpt-5.5
      api_key: ${OPENAI_API_KEY}
    storage: { backend: none }
    system_prompt: You are a helpful assistant powered by OpenAI.

  - id: claude-agent
    name: Claude Agent
    model:
      provider: anthropic
      model: claude-opus-4-8
      api_key: ${ANTHROPIC_API_KEY}
    storage: { backend: none }
    system_prompt: You are Claude, an AI assistant by Anthropic.

  - id: gemini-agent
    name: Gemini Agent
    model:
      provider: gemini
      model: gemini-2.0-flash
      api_key: ${GEMINI_API_KEY}
    storage: { backend: none }
    system_prompt: You are a helpful assistant powered by Google Gemini.

  - id: azure-agent
    name: Azure OpenAI Agent
    model:
      provider: azure
      deployment: my-gpt4o-deployment
      endpoint: https://my-resource.openai.azure.com
      api_version: "2024-10-21"
      api_key: ${AZURE_OPENAI_API_KEY}
    storage: { backend: none }
    system_prompt: You are a helpful assistant hosted on Azure OpenAI.

  - id: grok-agent
    name: Grok Agent
    model:
      provider: compatible          # xAI is OpenAI-compatible
      model: grok-4
      base_url: https://api.x.ai/v1
      api_key: ${XAI_API_KEY}
    storage: { backend: none }
    system_prompt: You are Grok, a witty assistant by xAI.

teams:
  - id: compare
    name: Provider Comparison
    strategy: parallel
    agents:
      - openai-agent
      - claude-agent
      - gemini-agent
      - azure-agent
      - grok-agent
    max_concurrency: 5
    error_strategy: best_effort      # missing keys don't fail the whole run
```

### Run it

```bash
# Set whichever provider keys you have — best_effort skips the rest.
export OPENAI_API_KEY=sk-your-key-here
export ANTHROPIC_API_KEY=sk-ant-your-key-here
export GEMINI_API_KEY=AI-your-key-here
export AZURE_OPENAI_API_KEY=your-azure-key
export XAI_API_KEY=xai-your-key-here

export CHRONOS_CONFIG=multi-provider.yaml

# Chat with an individual provider
chronos agent chat claude-agent
chronos agent chat grok-agent

# Fan the same question out to every provider in parallel
chronos team run compare "Explain quantum entanglement in 2 sentences"
```

The `error_strategy: best_effort` means if one provider fails (e.g., a missing API key), the other results are still returned — so you can start with just one or two keys.

---

## Provider Configurations

Every agent's `model:` block selects an LLM provider. Below is the exact YAML for
each supported provider — drop any of these into an agent (or into `defaults.model`).
All values support `${ENV_VAR}` expansion.

:::tip Ready-to-run per-provider files
Each provider below is also a standalone, single-agent config under
[`examples/yaml-configs/providers/`](https://github.com/spawn08/chronos/tree/main/examples/yaml-configs/providers)
— run any of them directly, e.g.:

```bash
export ANTHROPIC_API_KEY=sk-ant-...
chronos -c examples/yaml-configs/providers/anthropic.yaml run "Hello"
```
:::

| Provider | `provider:` value | Auth env var |
|----------|-------------------|--------------|
| OpenAI | `openai` | `OPENAI_API_KEY` |
| Anthropic (Claude) | `anthropic` | `ANTHROPIC_API_KEY` |
| Google Gemini (AI Studio) | `gemini` | `GEMINI_API_KEY` |
| Google Vertex AI | `compatible` | `GOOGLE_ACCESS_TOKEN` |
| Azure OpenAI | `azure` | `AZURE_OPENAI_API_KEY` |
| Grok (xAI) | `compatible` | `XAI_API_KEY` |
| Groq | `groq` | `GROQ_API_KEY` |
| Mistral | `mistral` | `MISTRAL_API_KEY` |
| DeepSeek | `deepseek` | `DEEPSEEK_API_KEY` |
| Ollama (local) | `ollama` | — (none) |
| Any OpenAI-compatible | `compatible` | `${YOUR_KEY}` |

### OpenAI

```yaml
model:
  provider: openai
  model: gpt-5.5
  api_key: ${OPENAI_API_KEY}
```

### Anthropic (Claude)

```yaml
model:
  provider: anthropic
  model: claude-opus-4-8
  api_key: ${ANTHROPIC_API_KEY}
```

### Google Gemini (AI Studio)

Use `provider: gemini` with an AI Studio API key — the simplest way to use Gemini.

```yaml
model:
  provider: gemini
  model: gemini-2.0-flash
  api_key: ${GEMINI_API_KEY}
```

### Google Vertex AI

Vertex is reached through its **OpenAI-compatible** endpoint, so use `provider: compatible`
with your project/location `base_url` and a short-lived access token
(`gcloud auth print-access-token`) as the key.

```yaml
model:
  provider: compatible
  model: google/gemini-2.0-flash
  base_url: https://us-central1-aiplatform.googleapis.com/v1/projects/YOUR_PROJECT/locations/us-central1/endpoints/openapi
  api_key: ${GOOGLE_ACCESS_TOKEN}
```

```bash
export GOOGLE_ACCESS_TOKEN=$(gcloud auth print-access-token)
```

### Azure OpenAI

Azure uses your **resource endpoint**, a **deployment name**, and an **API version** —
not a plain model id.

```yaml
model:
  provider: azure
  deployment: my-gpt4o-deployment      # the Azure *deployment* name
  endpoint: https://my-resource.openai.azure.com
  api_version: "2024-10-21"
  api_key: ${AZURE_OPENAI_API_KEY}
```

### Grok (xAI)

xAI's API is OpenAI-compatible, so use `provider: compatible` pointed at `https://api.x.ai/v1`.

```yaml
model:
  provider: compatible
  model: grok-4
  base_url: https://api.x.ai/v1
  api_key: ${XAI_API_KEY}
```

### Groq (ultra-fast inference)

The Groq endpoint is built in — just set the key and model.

```yaml
model:
  provider: groq
  model: llama-3.3-70b-versatile
  api_key: ${GROQ_API_KEY}
```

### Mistral

```yaml
model:
  provider: mistral
  model: mistral-large-latest
  api_key: ${MISTRAL_API_KEY}
```

### DeepSeek

```yaml
model:
  provider: deepseek
  model: deepseek-chat
  api_key: ${DEEPSEEK_API_KEY}
```

### Ollama (local, no API key)

```yaml
model:
  provider: ollama
  model: llama3.2
  base_url: http://localhost:11434
```

### Any OpenAI-compatible endpoint

`provider: compatible` (alias `custom`) works with Together, OpenRouter, Fireworks,
Perplexity, vLLM, LiteLLM, and anything that speaks the OpenAI Chat Completions API —
just set `base_url`.

```yaml
model:
  provider: compatible
  model: meta-llama/Llama-3.3-70B-Instruct-Turbo
  base_url: https://api.together.xyz/v1
  api_key: ${TOGETHER_API_KEY}
```

:::tip Mix providers in one team
Because each agent has its own `model:` block, a single `parallel` team can fan a
prompt out to OpenAI, Claude, Gemini, Azure, and Grok at once — see
[Example 5](#example-5-multi-provider-parallel-comparison).
:::

---

## Streaming output

The CLI commands (`chronos run`, `agent chat`, `team run`) **return the complete
response when the run finishes** — they don't print tokens as they arrive. So:

```bash
chronos -c content-pipeline.yaml team run pipeline "what can you do?"
# → prints the final composed answer once the pipeline completes
```

Chronos does support real-time streaming, through two mechanisms — both used from
Go rather than the CLI:

**1. Token streaming — `Provider.StreamChat`.** Every model provider returns a channel
of partial responses you can print as they arrive:

```go
// agent is an *agent.Agent you already built (or loaded from YAML).
import (
    "fmt"

    "github.com/spawn08/chronos/engine/model"
)

ch, _ := agent.Model.StreamChat(ctx, &model.ChatRequest{
    Messages: []model.Message{{Role: model.RoleUser, Content: "what can you do?"}},
})
for chunk := range ch {
    fmt.Print(chunk.Content) // print each delta as it streams in
}
```

**2. Run-event streaming — the SSE `Broker`.** The `StateGraph` runner publishes
node/graph/model events to a `stream.Broker`, which `chronos serve` exposes over
Server-Sent Events at `/api/events/stream` for dashboards and web clients.

The runnable [`examples/streaming_sse`](https://github.com/spawn08/chronos/tree/main/examples/streaming_sse)
example wires both together (run it with `go run ./examples/streaming_sse/`), and the
[Streaming &amp; SSE guide](/guides/streaming/) covers the full API.

---

## Team Strategies Reference

| Strategy | How it works | Best for |
|----------|-------------|----------|
| `sequential` | Agents run in order; each sees the previous agent's output | Pipelines: research → write → edit |
| `parallel` | Agents run concurrently on the same input | Getting multiple perspectives, comparisons |
| `router` | One agent is selected per request (LLM by default) | Customer support, intent-based dispatch |
| `coordinator` | A supervisor LLM plans and delegates sub-tasks | Complex projects needing decomposition |
| `swarm` | Agents hand off peer-to-peer until the task is done | Open-ended tasks with dynamic ownership |
| `hierarchy` | A root supervisor delegates to worker agents | Org-style delegation, multi-level teams |

### Team config fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique team identifier (used in `team run`) |
| `name` | string | Display name |
| `strategy` | string | `sequential`, `parallel`, `router`, `coordinator`, `swarm`, or `hierarchy` |
| `agents` | list | Agent IDs (order matters for sequential; entry agent for swarm) |
| `coordinator` | string | Agent ID for the coordinator strategy, or the root supervisor for `hierarchy` |
| `router` | string | Router mode: `model` (default) or `capability` (router strategy) |
| `router_model` | object | Optional model override for `router: model` (same fields as an agent `model`); defaults to the first member agent's model |
| `initial_agent` | string | Entry agent for the swarm strategy (defaults to the first listed agent) |
| `max_handoffs` | int | Max peer-to-peer handoffs (swarm strategy) |
| `max_concurrency` | int | Max parallel goroutines (parallel strategy) |
| `max_iterations` | int | Max coordinator planning loops |
| `error_strategy` | string | `fail_fast`, `collect`, or `best_effort` |

---

## CLI Commands Reference

### Agent commands

```bash
# List all agents from your config
chronos agent list

# Show details for a specific agent
chronos agent show <agent-id>

# Start interactive chat with an agent
chronos agent chat <agent-id>

# One-shot message to the default (first) agent
chronos run "your message here"

# One-shot message to a specific agent
chronos run --agent <agent-id> "your message here"
```

### Team commands

```bash
# List all teams
chronos team list

# Show team configuration
chronos team show <team-id>

# Run a team on a task
chronos team run <team-id> "your task description"
```

### Specifying a config file

A config file named anything (e.g. `content-pipeline.yaml`) is **not** auto-discovered —
only `.chronos/agents.yaml` (project) and `~/.chronos/agents.yaml` (global) are. For any
other filename, point the CLI at it:

```bash
# Option 1 (recommended): the -c / --config flag — works anywhere on the line
chronos -c /path/to/your-config.yaml team run my-team "do something"
chronos team show my-team --config content-pipeline.yaml

# Option 2: the CHRONOS_CONFIG environment variable
export CHRONOS_CONFIG=/path/to/your-config.yaml
chronos team run my-team "do something"

# Option 3: inline for a single command
CHRONOS_CONFIG=my-config.yaml chronos team run my-team "do something"
```

Resolution order: `-c/--config` → `CHRONOS_CONFIG` → `./.chronos/agents.yaml` → `~/.chronos/agents.yaml`.

---

## Running the Bundled Examples

The repository includes ready-to-run YAML configs in `examples/yaml-configs/`:

```bash
export OPENAI_API_KEY=sk-your-key-here

# Customer support — router dispatches to billing/technical/sales
CHRONOS_CONFIG=examples/yaml-configs/customer-support.yaml \
  chronos team run support "I need a refund for order #12345"

# Content pipeline — sequential research → write → edit
CHRONOS_CONFIG=examples/yaml-configs/content-pipeline.yaml \
  chronos team run pipeline "Write about renewable energy trends"

# Coding team — coordinator delegates to backend/frontend/reviewer
CHRONOS_CONFIG=examples/yaml-configs/coding-team.yaml \
  chronos team run dev-team "Build a REST API for user management"

# Multi-provider — parallel comparison of different LLMs
CHRONOS_CONFIG=examples/yaml-configs/multi-provider.yaml \
  chronos team run compare "What is the meaning of life?"
```

---

## Tips

- **Use `storage: backend: none` for team agents.** Agents in teams don't need their own database. This avoids creating unnecessary SQLite files.

- **Write detailed `description` fields.** The coordinator and the default `router: model` strategy use descriptions to decide which agent handles each task. Vague descriptions lead to poor routing.

- **Use `capabilities` tags.** They document each agent's role and drive the `router: capability` heuristic (matched against run-state keys). Be specific: `"api-design"` is better than `"development"`.

- **One config per use case.** Keep separate YAML files for different workflows rather than putting everything in one file.

- **Use `defaults` to avoid repetition.** Put your provider, API key, and storage settings in `defaults`. Individual agents only need to override what's different.
