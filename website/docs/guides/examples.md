---
title: "Examples"
---


# Examples Guide

Chronos ships with **25+ runnable examples** covering every major feature. Most require **no API keys** and run entirely with mock providers and SQLite. They are grouped by type so you can jump straight to what you need:

| Category | What it covers |
|----------|----------------|
| [Fundamentals](./examples/fundamentals.md) | Agent builder, tools, guardrails, graph patterns, memory, streaming — all no-key |
| [LLM Agents](./examples/llm-agents.md) | Live-LLM workflows: StateGraph reasoning, MCP tools, coding agent, multi-agent teams |
| [Providers & Models](./examples/providers.md) | Azure OpenAI, Vertex AI, Bedrock, multi-provider comparison, failover |
| [Durability & Sandboxing](./examples/durability.md) | Durable queue, human-in-the-loop resume, process sandboxing |
| [Enterprise & Multi-Tenancy](./examples/enterprise.md) | OIDC/JWKS SSO, data residency, per-tenant memory isolation |
| [Observability & CLI](./examples/observability-cli.md) | Metrics/cost/cache/retry hooks, CLI-driven agents and ops |
| [YAML Configs](./yaml-examples.md) | Declarative agent and team definitions |

## Running Examples

```bash
# Clone the repo
git clone https://github.com/spawn08/chronos.git
cd chronos

# Run any example
go run ./examples/<name>/
```

## Choosing a provider {#choosing-a-provider}

Every example that talks to a real LLM (`graph_with_llm`, `mcp_agent`, `coding_agent`, `team_deploy`, `multi_agent`, `multi_provider`) resolves its provider through a shared helper — `examples/internal/providers.Pick()` — which reads the environment and returns the first configured provider. **You never edit the example to switch clouds; you just export a different set of environment variables.**

| Provider | Environment variables |
|----------|-----------------------|
| **OpenAI** | `OPENAI_API_KEY` |
| **Anthropic** | `ANTHROPIC_API_KEY` |
| **Google Gemini** (AI Studio) | `GEMINI_API_KEY` |
| **Azure OpenAI** | `AZURE_OPENAI_API_KEY` + `AZURE_OPENAI_ENDPOINT` + `AZURE_OPENAI_DEPLOYMENT` (+ optional `AZURE_OPENAI_API_VERSION`) |
| **Google Cloud Vertex AI** | `GOOGLE_CLOUD_PROJECT` + `GOOGLE_ACCESS_TOKEN` (+ optional `GOOGLE_CLOUD_LOCATION`, `VERTEX_MODEL`) |
| **AWS Bedrock** | `AWS_REGION` + `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY` (+ optional `BEDROCK_MODEL_ID`) |
| **Mistral** | `MISTRAL_API_KEY` |
| **Ollama** (local) | `OLLAMA_HOST` (+ optional `OLLAMA_MODEL`) |
| **Any OpenAI-compatible** | `OPENAI_COMPATIBLE_BASE_URL` + `OPENAI_COMPATIBLE_MODEL` (+ optional `_API_KEY`, `_NAME`) — Together, Groq, DeepSeek, OpenRouter, Fireworks, Perplexity, Anyscale, vLLM, LiteLLM |

The four common cloud setups look like this — pick one, then run any real-LLM example:

```bash
# OpenAI
export OPENAI_API_KEY=sk-...

# Azure OpenAI
export AZURE_OPENAI_API_KEY=...
export AZURE_OPENAI_ENDPOINT=https://your-resource.openai.azure.com
export AZURE_OPENAI_DEPLOYMENT=gpt-4o          # your deployment name
export AZURE_OPENAI_API_VERSION=2024-12-01-preview

# Google Cloud Vertex AI (OpenAI-compatible endpoint + gcloud token)
export GOOGLE_CLOUD_PROJECT=my-gcp-project
export GOOGLE_CLOUD_LOCATION=us-central1
export VERTEX_MODEL=google/gemini-2.5-pro
export GOOGLE_ACCESS_TOKEN=$(gcloud auth print-access-token)

# AWS Bedrock
export AWS_REGION=us-east-1
export AWS_ACCESS_KEY_ID=...
export AWS_SECRET_ACCESS_KEY=...
export BEDROCK_MODEL_ID=anthropic.claude-3-5-sonnet-20241022-v2:0
```

:::tip
Precedence is fixed (OpenAI → Anthropic → Gemini → Azure → Vertex → Bedrock → Mistral → Ollama → compatible). To force a specific cloud when several key sets are present, unset the higher-precedence ones in that shell.
:::

## Full Index

### Fundamentals — no API key

| Example | Feature area |
|---------|-------------|
| [quickstart](./examples/fundamentals.md#quickstart) | Agent builder, SQLite, StateGraph |
| [chat_with_tools](./examples/fundamentals.md#chat_with_tools) | Tool calling loop |
| [tools_and_guardrails](./examples/fundamentals.md#tools_and_guardrails) | Permissions, guardrails |
| [graph_patterns](./examples/fundamentals.md#graph_patterns) | Conditional edges, interrupts, streaming |
| [memory_and_sessions](./examples/fundamentals.md#memory_and_sessions) | Short/long-term memory, sessions |
| [streaming_sse](./examples/fundamentals.md#streaming_sse) | Event broker, SSE |

### LLM Agents — API key (mock fallback)

| Example | Feature area |
|---------|-------------|
| [graph_with_llm](./examples/llm-agents.md#graph_with_llm) | StateGraph + live LLM |
| [mcp_agent](./examples/llm-agents.md#mcp_agent) | Model Context Protocol tools |
| [coding_agent](./examples/llm-agents.md#coding_agent) | Autonomous coding agent + RAG |
| [multi_agent](./examples/llm-agents.md#multi_agent) | 4 team strategies, bus delegation |
| [team_deploy](./examples/llm-agents.md#team_deploy) | YAML teams + sandbox deploy |

### Providers & Models

| Example | Feature area |
|---------|-------------|
| [multi_provider](./examples/providers.md#multi_provider) | Multiple providers side by side |
| [azure](./examples/providers.md#azure) | Azure OpenAI (chat + streaming) |
| [vertex](./examples/providers.md#vertex) | Google Cloud Vertex AI |
| [fallback_provider](./examples/providers.md#fallback_provider) | Provider failover |

### Durability & Sandboxing — no API key

| Example | Feature area |
|---------|-------------|
| [durable_queue](./examples/durability.md#durable_queue) | Leased workers, sleep, park/signal HITL, orphan recovery |
| [durable_hitl](./examples/durability.md#durable_hitl) | Human-in-the-loop approval with checkpoint + resume |
| [sandbox_execution](./examples/durability.md#sandbox_execution) | Process sandbox with timeouts and I/O capture |

### Enterprise & Multi-Tenancy

| Example | Feature area |
|---------|-------------|
| [enterprise_sso](./examples/enterprise.md#enterprise_sso) | ChronosOS behind OIDC/JWKS SSO |
| [data_residency](./examples/enterprise.md#data_residency) | Per-tenant storage routing (EU vs US) |
| [multitenant_memory](./examples/enterprise.md#multitenant_memory) | Per-tenant long-term memory isolation on one agent |

### Observability & CLI

| Example | Feature area |
|---------|-------------|
| [hooks_observability](./examples/observability-cli.md#hooks_observability) | Metrics, cost, cache, retry, rate limit |
| [cli_agent](./examples/observability-cli.md#cli_agent) | Build, inspect, run an agent from YAML via the CLI |
| [cli_ops](./examples/observability-cli.md#cli_ops) | Operate Chronos from the CLI (serve, monitor, db, sessions, pipe, deploy) |
