---
title: "Storage"
---


# Storage

Chronos uses pluggable storage adapters for persistent state. All adapters implement the same interface, allowing you to swap backends with zero code changes.

## Storage Interface

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

    // Event ledger (append-only)
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

The interface has 21 methods total. Every method is scoped to the tenant carried
by its `context.Context` (see [Multi-Tenancy](../guides/multi-tenancy.md)); callers
that never set a tenant operate under `storage.DefaultTenant`.

## Adapters

### SQLite (Development)

Single-file database with full interface implementation. Ideal for development, testing, and single-node deployments.

```go
import (
    "context"
    "log"

    "github.com/spawn08/chronos/storage/adapters/sqlite"
)

store, err := sqlite.New("myapp.db")
// or, for tests, an in-memory database:
// store, err := sqlite.New(":memory:")
if err != nil {
    log.Fatal(err)
}
defer store.Close()
if err := store.Migrate(context.Background()); err != nil { // creates all tables
    log.Fatal(err)
}
```

### PostgreSQL (Production)

Full-featured adapter for multi-node production deployments.

```go
import "github.com/spawn08/chronos/storage/adapters/postgres"

store, err := postgres.New("postgres://user:pass@host:5432/chronos?sslmode=require")
```

### In-memory

Map-backed adapter with no external dependencies. Suitable for unit tests only
(state is lost on process exit).

```go
import "github.com/spawn08/chronos/storage/adapters/memory"

store := memory.New() // *Store implements storage.Storage; New takes no args and returns no error
defer store.Close()
```

### Redis

High-throughput key-value storage using sorted sets for indexing, built on `redis/go-redis/v9`.

```go
import "github.com/spawn08/chronos/storage/adapters/redis"

store, err := redis.New("localhost:6379", "", 0) // addr, password, db
```

### MongoDB

Document-oriented storage, built on the official `go.mongodb.org/mongo-driver`.

```go
import "github.com/spawn08/chronos/storage/adapters/mongo"

store, err := mongo.New("mongodb://localhost:27017", "chronos") // uri, database
```

### DynamoDB

Serverless storage for AWS deployments, built on `aws-sdk-go-v2` with SigV4-signed
requests. Leave `accessKey`/`secretKey` empty to use the default AWS credential
chain (env, shared config, IAM role); pass a non-empty `endpoint` (e.g.
`http://localhost:8000`) to target DynamoDB Local.

```go
import "github.com/spawn08/chronos/storage/adapters/dynamo"

store, err := dynamo.New("", "chronos-table", "us-east-1", "", "") // endpoint, tableName, region, accessKey, secretKey
```

Note: unlike the other adapters, `dynamo.New` and `mongo.New`/`redis.New` are
Go-level constructors only — the YAML `storage.backend` config (below) does not
yet select them; wire them up with `agent.New(...).WithStorage(store)` in code.

## VectorStore Interface

For RAG pipelines and knowledge base search:

```go
type VectorStore interface {
    Upsert(ctx context.Context, collection string, embeddings []Embedding) error
    Search(ctx context.Context, collection string, query []float32, topK int, opts ...SearchOption) ([]SearchResult, error)
    Delete(ctx context.Context, collection string, ids []string) error
    CreateCollection(ctx context.Context, name string, dimension int) error
    Close() error
}
```

`Search` accepts optional `SearchOption`s; `storage.WithFilter(map[string]any{...})`
restricts results to embeddings whose metadata matches every key/value pair
(exact-match AND). Qdrant, pgvector, Pinecone, and ChromaDB apply the filter
server-side; LanceDB, Milvus, Weaviate, and RedisVector apply it client-side over
the returned window. See the [multi-tenancy guide](../guides/multi-tenancy.md).

### Vector Store Adapters

| Adapter | Import Path | Constructor |
|---------|-------------|-------------|
| Qdrant | `storage/adapters/qdrant` | `qdrant.New(baseURL string) *Store` |
| Pinecone | `storage/adapters/pinecone` | `pinecone.New(host, apiKey string) *Store` |
| Weaviate | `storage/adapters/weaviate` | `weaviate.New(endpoint, apiKey string) *Store` |
| Milvus | `storage/adapters/milvus` | `milvus.New(endpoint, token string) *Store` |
| Redis Vector | `storage/adapters/redisvector` | `redisvector.New(addr string) (*Store, error)` |
| pgvector | `storage/adapters/pgvector` | `pgvector.New(db *sql.DB) *Store` (requires the `vector` Postgres extension) |
| ChromaDB | `storage/adapters/chromadb` | `chromadb.New(baseURL string) *Store` |
| LanceDB | `storage/adapters/lancedb` | `lancedb.New(baseURL, apiKey, dbName string) *Store` |

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

Supported `storage.backend` values in YAML today: `sqlite`, `postgres` (alias
`postgresql`), and `none`/`memory` (no persistence). The `redis`, `mongo`, and
`dynamo` adapters exist as Go packages (see above) but are not yet wired into
the YAML backend switch — construct them directly and attach them with
`agent.New(...).WithStorage(store)`.

## Data Types

### Session

```go
type Session struct {
    ID        string
    TenantID  string         // omitted when using DefaultTenant
    AgentID   string
    Status    string         // "running", "paused", "completed", "failed"
    Metadata  map[string]any
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

### MemoryRecord

```go
type MemoryRecord struct {
    ID        string
    TenantID  string
    SessionID string    // empty for long-term memory
    AgentID   string
    UserID    string
    Kind      string    // "short_term" or "long_term"
    Key       string
    Value     any
    CreatedAt time.Time
}
```

### AuditLog

```go
type AuditLog struct {
    ID        string
    TenantID  string
    SessionID string
    Actor     string
    Action    string
    Resource  string
    Detail    map[string]any
    CreatedAt time.Time
}
```

### Trace

```go
type Trace struct {
    ID        string
    TenantID  string
    SessionID string
    ParentID  string
    Name      string
    Kind      string    // "node", "tool_call", "model_call", "approval"
    Input     any
    Output    any
    Error     string
    StartedAt time.Time
    EndedAt   time.Time
}
```

### Event

```go
type Event struct {
    ID        string
    TenantID  string
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
    TenantID  string
    SessionID string
    RunID     string
    NodeID    string
    State     map[string]any
    SeqNum    int64
    CreatedAt time.Time
}
```
