---
title: "Architecture"
permalink: /reference/architecture/
sidebar:
  nav: "docs"
---

# Architecture

Chronos is organized into four layers, each with clear responsibilities and boundaries.

## Layer Overview

```
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
```

## SDK Layer (`sdk/`)

The user-facing API for building agents and teams.

| Package | Purpose | Key Types |
|---------|---------|-----------|
| `sdk/agent` | Agent definition, builder, sessions, config | `Agent`, `Builder`, `ContextConfig` |
| `sdk/team` | Multi-agent orchestration | `Team`, `Strategy`, `RouterFunc` |
| `sdk/protocol` | Agent-to-agent communication | `Bus`, `Envelope`, `DirectChannel` |
| `sdk/memory` | Short/long-term memory with LLM extraction | `Store`, `Manager`, `MemoryTool` |
| `sdk/knowledge` | RAG: document loading and vector search | `Knowledge`, `VectorKnowledge` |
| `sdk/skill` | Skill metadata registry | `Skill`, `Registry` |

## Engine Layer (`engine/`)

The runtime components that power agent execution.

| Package | Purpose | Key Types |
|---------|---------|-----------|
| `engine/graph` | Durable StateGraph execution | `StateGraph`, `CompiledGraph`, `Runner`, `State` |
| `engine/model` | LLM provider implementations | `Provider`, `ChatRequest`, `ChatResponse`, `FallbackProvider` |
| `engine/tool` | Tool registry with permissions | `Registry`, `Definition`, `Handler`, `Permission` |
| `engine/guardrails` | Input/output validation | `Engine`, `Guardrail`, `BlocklistGuardrail`, `MaxLengthGuardrail` |
| `engine/hooks` | Before/after middleware chain | `Hook`, `Chain`, `MetricsHook`, `CostTracker`, `CacheHook`, `RetryHook`, `RateLimitHook` |
| `engine/stream` | SSE event broker | `Broker`, `Event` |

## Storage Layer (`storage/`)

Pluggable persistence with a single interface.

### Storage Interface

18 methods covering sessions, memory, audit logs, traces, events, and checkpoints.

| Adapter | Type | Use Case |
|---------|------|----------|
| SQLite | Storage | Development, testing, single-node |
| PostgreSQL | Storage | Production, multi-node |
| Redis | Storage | High-throughput, caching |
| MongoDB | Storage | Document-oriented workloads |
| DynamoDB | Storage | Serverless, AWS-native |

### VectorStore Interface

5 methods for vector embedding storage and similarity search.

| Adapter | Type | Use Case |
|---------|------|----------|
| Qdrant | VectorStore | Self-hosted vector DB |
| Pinecone | VectorStore | Managed vector DB |
| Weaviate | VectorStore | Hybrid search |
| Milvus | VectorStore | Large-scale vector search |
| Redis Vector | VectorStore | Redis with RediSearch |

## ChronosOS (`os/`)

HTTP control plane for production deployments.

| Package | Purpose |
|---------|---------|
| `os/server.go` | HTTP API: sessions, traces, SSE streaming, health |
| `os/auth` | Role-based access control |
| `os/approval` | Human-in-the-loop approval service |
| `os/trace` | Span collector for distributed tracing |

## Data Flow

### Chat (Single Turn)

```
User Message
    │
    ▼
Input Guardrails ──(blocked)──► Error
    │ (passed)
    ▼
Build Messages (system prompt + instructions + memories + knowledge + history)
    │
    ▼
Hook Chain: Before (logging → metrics → cost → rate limit → cache → retry)
    │
    ▼
Model Provider: Chat(ctx, request) ──► LLM API
    │
    ▼
Hook Chain: After (retry on error → cache store → cost accumulate → metrics record)
    │
    ▼
Tool Call Loop (if model requests tool calls)
    │
    ▼
Output Guardrails ──(blocked)──► Error
    │ (passed)
    ▼
Response
```

### Graph Execution

```
Initial State
    │
    ▼
Entry Node ──► Execute ──► Checkpoint ──► Find Next Edge
    │                                          │
    ▼                                          ▼
Stream Event                          Conditional? ──► Evaluate ──► Target Node
    │                                                              │
    ▼                                                              ▼
Interrupt? ──(yes)──► Checkpoint ──► Return (paused)        Loop back
    │ (no)
    ▼
End Node ──► Return (completed)
```

## Module Dependency Graph

```
cli/ ──► sdk/agent ──► engine/graph
                   ──► engine/model
                   ──► engine/tool
                   ──► engine/hooks
                   ──► engine/guardrails
                   ──► sdk/memory ──► storage/
                   ──► sdk/knowledge ──► storage/ + engine/model
                   ──► sdk/skill
                   ──► sdk/team ──► sdk/protocol
                   ──► storage/

os/ ──► storage/
    ──► engine/stream
```

No circular dependencies. Storage is the lowest layer; everything depends downward.
