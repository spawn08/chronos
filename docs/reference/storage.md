---
title: "Storage"
permalink: /reference/storage/
sidebar:
  nav: "docs"
---

# Storage

Chronos uses pluggable storage adapters for persistent state. All adapters implement the same interface, allowing you to swap backends with zero code changes.

## Storage Interface

```go
type Storage interface {
    // Sessions
    CreateSession(ctx context.Context, session *Session) error
    GetSession(ctx context.Context, id string) (*Session, error)
    UpdateSession(ctx context.Context, session *Session) error
    ListSessions(ctx context.Context, agentID string, limit, offset int) ([]*Session, error)
    DeleteSession(ctx context.Context, id string) error

    // Memory
    PutMemory(ctx context.Context, record *MemoryRecord) error
    GetMemory(ctx context.Context, agentID, key string) (*MemoryRecord, error)
    ListMemory(ctx context.Context, agentID, kind string) ([]*MemoryRecord, error)
    DeleteMemory(ctx context.Context, id string) error

    // Audit Logs
    InsertAuditLog(ctx context.Context, log *AuditLog) error
    ListAuditLogs(ctx context.Context, agentID string, limit int) ([]*AuditLog, error)

    // Traces
    InsertTrace(ctx context.Context, trace *Trace) error
    ListTraces(ctx context.Context, sessionID string) ([]*Trace, error)

    // Events (Append-Only Ledger)
    AppendEvent(ctx context.Context, event *Event) error
    ListEvents(ctx context.Context, sessionID string, afterSeq int64) ([]*Event, error)

    // Checkpoints
    SaveCheckpoint(ctx context.Context, checkpoint *Checkpoint) error
    GetCheckpoint(ctx context.Context, id string) (*Checkpoint, error)
    GetLatestCheckpoint(ctx context.Context, sessionID string) (*Checkpoint, error)

    // Lifecycle
    Migrate(ctx context.Context) error
    Close() error
}
```

## Adapters

### SQLite (Development)

Single-file database with full interface implementation. Ideal for development, testing, and single-node deployments.

```go
import "github.com/spawn08/chronos/storage/adapters/sqlite"

store, err := sqlite.New("myapp.db")
// or in-memory:
store, err := sqlite.New(":memory:")

defer store.Close()
store.Migrate(ctx) // creates all tables
```

### PostgreSQL (Production)

Full-featured adapter for multi-node production deployments.

```go
import "github.com/spawn08/chronos/storage/adapters/postgres"

store, err := postgres.New("postgres://user:pass@host:5432/chronos?sslmode=require")
```

### Redis

High-throughput key-value storage using sorted sets for indexing.

```go
import "github.com/spawn08/chronos/storage/adapters/redis"

store, err := redis.New("localhost:6379", "", 0)
```

### MongoDB

Document-oriented storage.

```go
import "github.com/spawn08/chronos/storage/adapters/mongo"

store, err := mongo.New("mongodb://localhost:27017", "chronos")
```

### DynamoDB

Serverless storage for AWS deployments.

```go
import "github.com/spawn08/chronos/storage/adapters/dynamo"

store, err := dynamo.New("us-east-1", "chronos-table")
```

## VectorStore Interface

For RAG pipelines and knowledge base search:

```go
type VectorStore interface {
    Upsert(ctx context.Context, collection string, embeddings []Embedding) error
    Search(ctx context.Context, collection string, query []float32, topK int) ([]SearchResult, error)
    Delete(ctx context.Context, collection string, ids []string) error
    CreateCollection(ctx context.Context, name string, dimension int) error
    Close() error
}
```

### Vector Store Adapters

| Adapter | Import Path |
|---------|-------------|
| Qdrant | `storage/adapters/qdrant` |
| Pinecone | `storage/adapters/pinecone` |
| Weaviate | `storage/adapters/weaviate` |
| Milvus | `storage/adapters/milvus` |
| Redis Vector | `storage/adapters/redisvector` |

## YAML Configuration

Storage can be configured in YAML:

```yaml
defaults:
  storage:
    backend: sqlite
    dsn: chronos.db

agents:
  - id: production-agent
    storage:
      backend: postgres
      dsn: ${DATABASE_URL}
```

Supported backend values: `sqlite`, `postgres`, `redis`, `mongo`, `dynamo`.

## Data Types

### Session

```go
type Session struct {
    ID        string
    AgentID   string
    UserID    string
    Status    string    // "active", "ended"
    Metadata  map[string]any
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

### MemoryRecord

```go
type MemoryRecord struct {
    ID        string
    SessionID string
    AgentID   string
    Kind      string    // "short_term" or "long_term"
    Key       string
    Value     any
    CreatedAt time.Time
}
```

### Event

```go
type Event struct {
    ID        string
    SessionID string
    SeqNum    int64
    Type      string    // "chat_message", "chat_summary", "node_executed", etc.
    Payload   any       // JSON-serializable
    CreatedAt time.Time
}
```

### Checkpoint

```go
type Checkpoint struct {
    ID        string
    SessionID string
    RunID     string
    NodeID    string
    State     map[string]any
    SeqNum    int64
    CreatedAt time.Time
}
```
