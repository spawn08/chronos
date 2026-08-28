---
title: "Interfaces"
---


This page lists the core interfaces in Chronos. Implement these to extend the framework with custom providers, storage backends, guardrails, hooks, and sandboxes.

Every signature below uses `context.Context` from the standard library (and `time.Duration` in `Sandbox`). All other types (`ChatRequest`, `Session`, `Embedding`, `Document`, `Result`, `Event`, …) are declared in the package named beneath each interface heading — import that package to implement the interface. For example:

```go
import "github.com/spawn08/chronos/engine/model" // for model.Provider, model.EmbeddingsProvider, model.TokenCounter
```

## model.Provider

The LLM provider interface. All model backends implement this.

**Package:** `engine/model`

```go
type Provider interface {
    Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
    StreamChat(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error)
    Name() string
    Model() string
}
```

| Method | Description |
|--------|-------------|
| `Chat` | Send a request and receive a complete response |
| `StreamChat` | Return a channel of partial responses for streaming |
| `Name` | Human-readable provider name (e.g., "openai") |
| `Model` | Default model ID (e.g., "gpt-5.5") |

**Implementations:** `OpenAI`, `Anthropic`, `Gemini`, `Mistral`, `Ollama`, `AzureOpenAI`, `OpenAICompatible`, `FallbackProvider`

---

## model.EmbeddingsProvider

Provider for text embeddings, used in RAG pipelines.

**Package:** `engine/model`

```go
type EmbeddingsProvider interface {
    Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)
}
```

**Implementations:** `OpenAIEmbeddings`, `OllamaEmbeddings`, `CachedEmbeddings`

---

## model.TokenCounter

Estimates token counts for messages.

**Package:** `engine/model`

```go
type TokenCounter interface {
    CountTokens(messages []Message) int
    CountString(s string) int
}
```

**Implementations:** `EstimatingCounter` (4-chars-per-token heuristic), `BPECounter` (BPE tokenizer, falls back to `EstimatingCounter` if unavailable)

---

## storage.Storage

The primary persistence interface. All storage adapters implement this (18 methods).

**Package:** `storage`

```go
type Storage interface {
    // Sessions
    CreateSession(ctx context.Context, s *Session) error
    GetSession(ctx context.Context, id string) (*Session, error)
    UpdateSession(ctx context.Context, s *Session) error
    ListSessions(ctx context.Context, agentID string, limit, offset int) ([]*Session, error)

    // Memory
    PutMemory(ctx context.Context, m *MemoryRecord) error
    GetMemory(ctx context.Context, agentID, key string) (*MemoryRecord, error)
    ListMemory(ctx context.Context, agentID string, kind string) ([]*MemoryRecord, error)
    DeleteMemory(ctx context.Context, id string) error

    // Audit logs
    AppendAuditLog(ctx context.Context, log *AuditLog) error
    ListAuditLogs(ctx context.Context, sessionID string, limit, offset int) ([]*AuditLog, error)

    // Traces
    InsertTrace(ctx context.Context, t *Trace) error
    GetTrace(ctx context.Context, id string) (*Trace, error)
    ListTraces(ctx context.Context, sessionID string) ([]*Trace, error)

    // Event ledger
    AppendEvent(ctx context.Context, e *Event) error
    ListEvents(ctx context.Context, sessionID string, afterSeq int64) ([]*Event, error)

    // Checkpoints
    SaveCheckpoint(ctx context.Context, cp *Checkpoint) error
    GetCheckpoint(ctx context.Context, id string) (*Checkpoint, error)
    GetLatestCheckpoint(ctx context.Context, sessionID string) (*Checkpoint, error)
    ListCheckpoints(ctx context.Context, sessionID string) ([]*Checkpoint, error)

    // Lifecycle
    Migrate(ctx context.Context) error
    Close() error
}
```

**Implementations:** `sqlite.Store`, `postgres.Store`, `redis.Store`, `mongo.Store`, `dynamo.Store`, `memory.Store` (in-memory, for tests)

---

## storage.VectorStore

Vector storage for embeddings, used by the knowledge/RAG system.

**Package:** `storage`

```go
type VectorStore interface {
    Upsert(ctx context.Context, collection string, embeddings []Embedding) error
    Search(ctx context.Context, collection string, query []float32, topK int, opts ...SearchOption) ([]SearchResult, error)
    Delete(ctx context.Context, collection string, ids []string) error
    CreateCollection(ctx context.Context, name string, dimension int) error
    Close() error
}
```

`Search`'s variadic `opts` carry optional `SearchOption`s — currently just `WithFilter(filter map[string]any)`, which restricts results to embeddings whose metadata matches every key/value pair (exact match, AND semantics). Adapters that store metadata structurally (`qdrant`, `pgvector`, `pinecone`, `chromadb`) apply the filter server-side, so top-k is computed over the matching subset. Adapters that store metadata as an opaque blob (`lancedb`, `milvus`, `weaviate`, `redisvector`) apply it client-side over the returned window instead, which can under-return results under a highly selective filter. This is the mechanism that powers multitenant memory recall — e.g. scoping a shared collection by a tenant token.

**Implementations:** `qdrant.Store`, `pinecone.Store`, `weaviate.Store`, `milvus.Store`, `redisvector.Store`, `pgvector.Store`, `lancedb.Store`, `chromadb.Store`

---

## knowledge.Knowledge

The RAG knowledge base interface.

**Package:** `sdk/knowledge`

```go
type Knowledge interface {
    Load(ctx context.Context) error
    Search(ctx context.Context, query string, topK int) ([]Document, error)
    Close() error
}
```

**Implementations:** `VectorKnowledge`

---

## guardrails.Guardrail

Input/output validation interface.

**Package:** `engine/guardrails`

```go
type Guardrail interface {
    Check(ctx context.Context, content string) Result
}
```

`Result` has fields `Passed bool` and `Reason string`.

**Implementations:** `BlocklistGuardrail`, `MaxLengthGuardrail`

:::caution
`PIIGuardrail` (`engine/guardrails/pii.go`) and `InjectionGuardrail` (`engine/guardrails/injection.go`) do **not** currently satisfy this interface — their `Check` methods are `Check(_ interface{}, content string) *Result` (wrong parameter type, pointer return) instead of `Check(ctx context.Context, content string) Result`. They're usable directly (`g.Check(nil, content)`), just not as a `Guardrail` value. See [guides/guardrails.md](/guides/guardrails) for details.
:::

---

## hooks.Hook

Middleware interface for intercepting execution events.

**Package:** `engine/hooks`

```go
type Hook interface {
    Before(ctx context.Context, evt *Event) error
    After(ctx context.Context, evt *Event) error
}
```

**Implementations:** `LoggingHook`, `RetryHook`, `RateLimitHook`, `CostTracker`, `CacheHook`, `MetricsHook`, `PrometheusHook`, `Chain`

---

## sandbox.Sandbox

Isolated execution interface for running untrusted code.

**Package:** `sandbox`

```go
type Sandbox interface {
    Execute(ctx context.Context, command string, args []string, timeout time.Duration) (*Result, error)
    Close() error
}
```

**Implementations:** `ProcessSandbox`, `ContainerSandbox`, `WASMSandbox`, `K8sJobSandbox`
