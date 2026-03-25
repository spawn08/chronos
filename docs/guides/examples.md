---
title: "Examples"
permalink: /guides/examples/
sidebar:
  nav: "docs"
---

# Examples Guide

Chronos ships with 12+ runnable examples covering every major feature. Most require **no API keys** and run entirely with mock providers and SQLite.

## Running Examples

```bash
# Clone the repo
git clone https://github.com/spawn08/chronos.git
cd chronos

# Run any example
go run ./examples/<name>/
```

## Real LLM Examples

These examples make actual LLM API calls. Set at least one API key to run them.

### graph_with_llm

**StateGraph with real LLM calls inside nodes.** This is the most important example for understanding how Chronos combines graph workflows with live LLM reasoning. A classifier node calls the LLM to categorize questions, then conditional edges route to a technical expert (with tools) or a general assistant.

```bash
export OPENAI_API_KEY=sk-your-key
go run ./examples/graph_with_llm/
```

**Demonstrates:**
- Wiring real LLM providers (OpenAI, Anthropic, Gemini, Ollama) into graph nodes
- Conditional routing based on LLM classification output
- Tool calling within graph nodes
- Checkpointing with SQLite
- The YAML equivalent (see `examples/yaml-configs/graph-agent.yaml`)

See the [Building Real-World Agents](/guides/real-world-agents/) guide for a detailed walkthrough.

---

## No API Keys Required

These examples use mock providers — they compile and run instantly:

### quickstart

Minimal agent with SQLite storage and a 3-node StateGraph (greet → classify → respond).

```bash
go run ./examples/quickstart/
```

**Demonstrates:** Agent builder, SQLite storage, graph nodes, `Run()` method.

---

### tools_and_guardrails

Tool registry with three permission levels (allow, deny, require_approval) and input/output guardrails.

```bash
go run ./examples/tools_and_guardrails/
```

**Demonstrates:**
- Registering tools with handlers and JSON Schema parameters
- `tool.PermAllow` — auto-executed tools (calculator, weather)
- `tool.PermDeny` — blocked tools (delete_database)
- `tool.PermRequireApproval` — tools requiring human approval (send_email)
- `BlocklistGuardrail` — blocks inputs containing prohibited terms
- `MaxLengthGuardrail` — limits output length
- Approval handler callback

---

### hooks_observability

All 6 hooks in action: metrics, cost tracking, rate limiting, caching, retry, and structured logging.

```bash
go run ./examples/hooks_observability/
```

**Demonstrates:**
- `MetricsHook` — latency tracking, call counting, per-call metrics
- `CostTracker` — per-model pricing, budget limits, token accounting
- `CacheHook` — LLM response caching with TTL and max entries
- `RateLimitHook` — token-bucket rate limiting
- `RetryHook` — exponential backoff with retry callbacks
- `LoggingHook` — structured event logging
- Hook chain composition

---

### graph_patterns

StateGraph patterns: conditional edges, interrupt nodes (human-in-the-loop), stream events, and multi-path routing.

```bash
go run ./examples/graph_patterns/
```

**Demonstrates:**
- `AddConditionalEdge` — dynamic routing based on state (e.g., order validation)
- `AddInterruptNode` — pauses execution for human approval with checkpoint
- `graph.NewRunner` + `runner.Stream()` — real-time execution events
- Multi-path graphs with convergence (support ticket triage)
- Checkpoint persistence for resume

---

### memory_and_sessions

Short-term and long-term memory APIs, plus multi-turn persistent sessions.

```bash
go run ./examples/memory_and_sessions/
```

**Demonstrates:**
- `memory.NewStore` — short-term (session-scoped) and long-term (cross-session) memory
- `SetShortTerm`, `SetLongTerm`, `Get`, `ListShortTerm`, `ListLongTerm`
- `ChatWithSession` — persistent multi-turn conversations
- Session lifecycle: creation, event ledger, listing
- Multiple sessions per agent

---

### streaming_sse

Event broker for real-time observability: pub/sub, graph stream events, and SSE HTTP handler.

```bash
go run ./examples/streaming_sse/
```

**Demonstrates:**
- `stream.NewBroker` — publish/subscribe event system
- Multiple subscribers receiving the same events
- Graph runner stream events (`node_start`, `node_end`, `edge_transition`, `completed`)
- `SSEHandler` — HTTP endpoint for Server-Sent Events
- Integration pattern for real-time dashboards

---

### chat_with_tools

Agent with tool definitions: direct tool execution and model-aware tool passing.

```bash
go run ./examples/chat_with_tools/
```

**Demonstrates:**
- Calculator tool with expression parsing
- Geography lookup tool
- Direct tool execution via `agent.Tools.Execute()`
- Tool definitions automatically passed to model in `Chat()` requests
- JSON Schema tool parameter definitions

---

### fallback_provider

Automatic failover between model providers with configurable callbacks.

```bash
go run ./examples/fallback_provider/
```

**Demonstrates:**
- `model.NewFallbackProvider(primary, secondary, local)` — provider chain
- `OnFallback` callback for monitoring failures
- Primary succeeds → no fallback needed
- Primary fails → secondary used transparently
- All providers fail → graceful error reporting
- Streaming fallback
- Zero providers → validation error

---

### sandbox_execution

Process sandbox for running untrusted commands with timeouts and output capture.

```bash
go run ./examples/sandbox_execution/
```

**Demonstrates:**
- `sandbox.NewProcessSandbox(workDir)` — isolated execution environment
- Stdout/stderr capture
- Exit code handling
- Timeout enforcement (10s command killed after 500ms)
- File I/O within the sandbox working directory
- Environment variable access

---

## API Keys Required

These examples connect to real LLM APIs:

### multi_agent

All 4 team strategies (sequential, parallel, router, coordinator), direct channels, and bus delegation. Works with mock provider if no API key is set.

```bash
# With mock (no key)
go run ./examples/multi_agent/

# With real LLM
OPENAI_API_KEY=sk-... go run ./examples/multi_agent/
```

---

### multi_provider

Connects to multiple LLM providers side by side.

```bash
OPENAI_API_KEY=sk-... go run ./examples/multi_provider/
```

---

### azure

Azure OpenAI provider with standard and streaming modes.

```bash
export AZURE_OPENAI_API_KEY=...
export AZURE_OPENAI_ENDPOINT=https://your-resource.openai.azure.com
export AZURE_OPENAI_DEPLOYMENT=gpt-4o
export AZURE_OPENAI_API_VERSION=2024-12-01-preview
go run ./examples/azure/
go run ./examples/azure/ -stream
```

---

## YAML Configs

Pre-built YAML configurations for common patterns:

| Config | Strategy | Description |
|--------|----------|-------------|
| `graph-agent.yaml` | Router | LLM-based classification routing (YAML equivalent of `graph_with_llm/`) |
| `customer-support.yaml` | Router | Routes queries to billing, technical, or sales agents |
| `content-pipeline.yaml` | Sequential | Research → Write → Edit article pipeline |
| `coding-team.yaml` | Coordinator | Tech lead delegates to backend, frontend, reviewer |
| `multi-provider.yaml` | Mixed | OpenAI, Anthropic, Gemini, Ollama, Groq, DeepSeek |
| `sandbox-deploy.yaml` | Sequential / Coordinator | Multi-agent sandbox deployment |

Use with the CLI:

```bash
CHRONOS_CONFIG=examples/yaml-configs/customer-support.yaml go run ./cli/main.go repl
```
