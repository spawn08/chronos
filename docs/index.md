---
layout: single
title: Chronos
permalink: /
sidebar:
  nav: "docs"
toc: false
classes: wide
---

<div class="hero">
  <h1>Chronos</h1>
  <p class="tagline">
    A Go framework for building durable, scalable AI agents.<br/>
    Define agents in YAML. Connect any LLM. Let them collaborate.
  </p>
  <div class="hero-buttons">
    <a href="{{ '/getting-started/quickstart/' | relative_url }}" class="btn btn-primary">Get Started</a>
    <a href="https://github.com/spawn08/chronos" class="btn btn-outline">
      <i class="fab fa-github"></i> GitHub
    </a>
  </div>
  <div class="install-command">
    <span class="prompt">$ </span>go get github.com/spawn08/chronos
  </div>
</div>

<div class="stats-bar">
  <div class="stat">
    <div class="stat-number">14+</div>
    <div class="stat-label">LLM Providers</div>
  </div>
  <div class="stat">
    <div class="stat-number">10</div>
    <div class="stat-label">Storage Adapters</div>
  </div>
  <div class="stat">
    <div class="stat-number">4</div>
    <div class="stat-label">Team Strategies</div>
  </div>
  <div class="stat">
    <div class="stat-number">6</div>
    <div class="stat-label">Middleware Hooks</div>
  </div>
</div>

---

## Why Chronos?

Agentic software is fundamentally different from traditional request-response systems. Agents reason, call tools, pause for human approval, and resume later. They collaborate with other agents, maintain memory across sessions, and make decisions under uncertainty.

Chronos provides the **full stack** for building this kind of software in Go:

| Layer | Responsibility |
|-------|---------------|
| **SDK** | Agent builder, skills, memory, knowledge, teams, inter-agent protocol |
| **Engine** | StateGraph runtime, model providers, tool registry, guardrails, streaming |
| **ChronosOS** | HTTP control plane, auth, tracing, audit logs, approval enforcement |
| **Storage** | Pluggable persistence for sessions, checkpoints, memory, vectors |

---

## Key Features

<div class="feature-grid">
  <div class="feature-card">
    <h3><span class="feature-icon">&#x2699;</span> YAML-First Config</h3>
    <p>Define agents, teams, and models in <code>.chronos/agents.yaml</code> with environment variable expansion and defaults inheritance. Run from CLI with zero Go code.</p>
  </div>
  <div class="feature-card">
    <h3><span class="feature-icon">&#x1F916;</span> 14+ LLM Providers</h3>
    <p>OpenAI, Anthropic, Gemini, Mistral, Ollama, Azure OpenAI, Groq, DeepSeek, and any OpenAI-compatible endpoint. Swap with one line.</p>
  </div>
  <div class="feature-card">
    <h3><span class="feature-icon">&#x1F465;</span> Multi-Agent Teams</h3>
    <p>Sequential, parallel, router, and coordinator strategies. Define teams in YAML and run from the CLI.</p>
  </div>
  <div class="feature-card">
    <h3><span class="feature-icon">&#x1F50C;</span> Durable Execution</h3>
    <p>StateGraph runtime with checkpointing, interrupt nodes, and resume. Survive crashes and restart exactly where you left off.</p>
  </div>
  <div class="feature-card">
    <h3><span class="feature-icon">&#x1F6E1;</span> Guardrails & Hooks</h3>
    <p>Input/output validation, retry, rate limiting, cost tracking, caching, and observability. All composable via middleware.</p>
  </div>
  <div class="feature-card">
    <h3><span class="feature-icon">&#x1F9E0;</span> Memory & RAG</h3>
    <p>Short-term and long-term memory with LLM-powered extraction. Vector-backed retrieval injected into agent context.</p>
  </div>
  <div class="feature-card">
    <h3><span class="feature-icon">&#x1F4E6;</span> 10 Storage Adapters</h3>
    <p>SQLite, PostgreSQL, Redis, MongoDB, DynamoDB, Qdrant, Pinecone, Weaviate, Milvus. One interface, any backend.</p>
  </div>
  <div class="feature-card">
    <h3><span class="feature-icon">&#x1F4AC;</span> Context Summarization</h3>
    <p>Automatic conversation summarization when approaching token limits. Rolling summaries preserve key facts within the context window.</p>
  </div>
  <div class="feature-card">
    <h3><span class="feature-icon">&#x1F680;</span> Production Ready</h3>
    <p>Docker, Helm chart with HPA and Ingress, CI/CD with GitHub Actions, cross-platform binaries.</p>
  </div>
</div>

---

## Architecture

<div class="arch-diagram">
<pre>
┌──────────────────────────────────────────────────────────────┐
│                   ChronosOS  (Control Plane)                 │
│   Auth & RBAC  │  Tracing & Audit  │  Approval  │  HTTP API │
└────────────────────────────┬─────────────────────────────────┘
                             │
┌────────────────────────────▼─────────────────────────────────┐
│                         Engine                               │
│  StateGraph Runtime │ Model Providers │ Tools │ Guardrails   │
│  Hooks & Middleware │ SSE Streaming                          │
└────────────────────────────┬─────────────────────────────────┘
                             │
┌────────────────────────────▼─────────────────────────────────┐
│                          SDK                                 │
│  Agent Builder │ Teams │ Protocol Bus │ Skills │ Memory/RAG  │
└────────────────────────────┬─────────────────────────────────┘
                             │
┌────────────────────────────▼─────────────────────────────────┐
│                    Storage  (Pluggable)                       │
│  SQLite │ PostgreSQL │ Redis │ MongoDB │ DynamoDB            │
│  Qdrant │ Pinecone │ Weaviate │ Milvus                      │
└──────────────────────────────────────────────────────────────┘
</pre>
</div>

---

## Quick Start

**YAML approach** — define an agent and run it:

```yaml
# .chronos/agents.yaml
agents:
  - id: assistant
    name: Assistant
    model:
      provider: openai
      model: gpt-4o
      api_key: ${OPENAI_API_KEY}
    system_prompt: You are a helpful assistant.
```

```bash
export OPENAI_API_KEY=sk-...
go run ./cli/main.go run "What is the capital of France?"
```

**Go builder** — for programmatic control:

```go
a, _ := agent.New("chat", "Chat Agent").
    WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
    WithSystemPrompt("You are a helpful assistant.").
    Build()

resp, _ := a.Chat(ctx, "What is the capital of France?")
fmt.Println(resp.Content)
```

<div class="hero-buttons" style="margin-top: 2rem;">
  <a href="{{ '/getting-started/quickstart/' | relative_url }}" class="btn btn-primary">Read the Quickstart</a>
  <a href="{{ '/guides/yaml-examples/' | relative_url }}" class="btn btn-outline">YAML Examples</a>
  <a href="{{ '/guides/agents/' | relative_url }}" class="btn btn-outline">Explore the Docs</a>
</div>
