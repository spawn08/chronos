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
    Define agents. Connect any LLM. Let them collaborate.
  </p>
  <div class="hero-buttons">
    <a href="{{ '/getting-started/quickstart/' | relative_url }}" class="btn btn-primary">Get Started</a>
    <a href="https://github.com/chronos-ai/chronos" class="btn btn-outline">
      <i class="fab fa-github"></i> View on GitHub
    </a>
  </div>
  <div class="install-command">
    <span class="prompt">$ </span>go get github.com/chronos-ai/chronos
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
    <h3><span class="feature-icon">&#x2699;</span> YAML-First Configuration</h3>
    <p>Define agents, models, and storage in <code>.chronos/agents.yaml</code> with environment variable expansion and defaults inheritance. No Go code required for basic setups.</p>
  </div>
  <div class="feature-card">
    <h3><span class="feature-icon">&#x1F916;</span> Multi-Provider LLM Support</h3>
    <p>OpenAI, Anthropic, Google Gemini, Mistral, Ollama, Azure OpenAI, and any OpenAI-compatible endpoint. Swap providers with one line.</p>
  </div>
  <div class="feature-card">
    <h3><span class="feature-icon">&#x1F4AC;</span> Context Summarization</h3>
    <p>Automatic conversation summarization when the token limit is approached. Rolling summaries preserve key facts while staying within the context window.</p>
  </div>
  <div class="feature-card">
    <h3><span class="feature-icon">&#x1F50C;</span> Durable Execution</h3>
    <p>StateGraph runtime with checkpointing, interrupt nodes, and resume-from-checkpoint. Survive crashes and restart exactly where you left off.</p>
  </div>
  <div class="feature-card">
    <h3><span class="feature-icon">&#x1F465;</span> Multi-Agent Teams</h3>
    <p>Sequential, parallel, router, and coordinator strategies with a typed protocol bus for task delegation, questions, handoffs, and broadcasts.</p>
  </div>
  <div class="feature-card">
    <h3><span class="feature-icon">&#x1F6E1;</span> Guardrails & Middleware</h3>
    <p>Input/output validation, retry with backoff, rate limiting, cost tracking, response caching, and observability metrics. All composable via hooks.</p>
  </div>
  <div class="feature-card">
    <h3><span class="feature-icon">&#x1F9E0;</span> Memory & Knowledge</h3>
    <p>Short-term and long-term memory with LLM-powered extraction. Vector-backed RAG automatically injected into agent context.</p>
  </div>
  <div class="feature-card">
    <h3><span class="feature-icon">&#x1F4E6;</span> Pluggable Storage</h3>
    <p>Single interface with adapters for SQLite, PostgreSQL, Redis, MongoDB, DynamoDB, Qdrant, Pinecone, Weaviate, and Milvus.</p>
  </div>
  <div class="feature-card">
    <h3><span class="feature-icon">&#x1F680;</span> Production Ready</h3>
    <p>Docker images, Helm chart with HPA, Ingress, Secrets, and ServiceAccount. CI/CD with GitHub Actions. Cross-platform binaries.</p>
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

## Quick Example

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/chronos-ai/chronos/engine/model"
    "github.com/chronos-ai/chronos/sdk/agent"
)

func main() {
    a, _ := agent.New("chat-agent", "Chat Agent").
        WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
        WithSystemPrompt("You are a helpful assistant.").
        Build()

    resp, _ := a.Chat(context.Background(), "What is the capital of France?")
    fmt.Println(resp.Content)
}
```

<div class="hero-buttons" style="margin-top: 2rem;">
  <a href="{{ '/getting-started/quickstart/' | relative_url }}" class="btn btn-primary">Read the Quickstart Guide</a>
  <a href="{{ '/guides/agents/' | relative_url }}" class="btn btn-outline">Explore the Docs</a>
</div>
