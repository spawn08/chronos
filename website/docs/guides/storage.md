---
title: "Storage Adapters"
---


Chronos defines a single `Storage` interface with 21 methods covering sessions, memory, audit logs, traces, events, and checkpoints. All adapters implement the same contract, so you can swap backends with zero code changes.

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

## Available Adapters

| Adapter | Type | Package | Status |
|---------|------|---------|--------|
| SQLite | Storage | `storage/adapters/sqlite` | Production-ready |
| PostgreSQL | Storage | `storage/adapters/postgres` | Production-ready |
| In-memory | Storage | `storage/adapters/memory` | Testing/dev only (no persistence) |
| Redis | Storage | `storage/adapters/redis` | Available (go-redis/v9) |
| MongoDB | Storage | `storage/adapters/mongo` | Available (official driver) |
| DynamoDB | Storage | `storage/adapters/dynamo` | Available (aws-sdk-go-v2) |
| Qdrant | VectorStore | `storage/adapters/qdrant` | Production-ready |
| Pinecone | VectorStore | `storage/adapters/pinecone` | Available |
| Weaviate | VectorStore | `storage/adapters/weaviate` | Available |
| Milvus | VectorStore | `storage/adapters/milvus` | Available |
| Redis Vector | VectorStore | `storage/adapters/redisvector` | Available (go-redis/v9) |
| pgvector | VectorStore | `storage/adapters/pgvector` | Available (requires the Postgres `vector` extension) |
| ChromaDB | VectorStore | `storage/adapters/chromadb` | Available (REST API) |
| LanceDB | VectorStore | `storage/adapters/lancedb` | Available (REST API) |

The Redis, MongoDB, DynamoDB, and Redis Vector adapters are built on their
respective **official Go SDKs** (`redis/go-redis/v9`, `go.mongodb.org/mongo-driver`,
`aws-sdk-go-v2`) for correct wire-protocol handling, connection pooling, retries,
and auth (DynamoDB uses SigV4).

:::note Breaking change
The MongoDB constructor is now `mongo.New(uri, database)` (previously it took the
deprecated Atlas Data API base URL, API key, database, and data source). Use a
standard MongoDB connection string, e.g. `mongo.New("mongodb://localhost:27017", "chronos")`.
:::

:::tip Multi-tenancy
All records carry a `tenant_id`, and reads/writes can be scoped per tenant with
`storage.WithTenant(ctx, id)`. See the [Multi-Tenancy guide](./multi-tenancy.md).
:::

## SQLite (Development)

The default adapter for development and testing. Data is stored in a single file.

```go
import (
    "context"
    "log"

    "github.com/spawn08/chronos/storage/adapters/sqlite"
)

func main() {
    store, err := sqlite.New("chronos.db")
    if err != nil {
        log.Fatal(err)
    }
    defer store.Close()

    if err := store.Migrate(context.Background()); err != nil {
        log.Fatal(err)
    }
}
```

For in-memory testing, use `":memory:"`:

```go
store, _ := sqlite.New(":memory:")
```

## PostgreSQL (Production)

The recommended adapter for production deployments.

```go
import (
    "context"
    "log"

    "github.com/spawn08/chronos/storage/adapters/postgres"
)

func main() {
    store, err := postgres.New("postgres://user:pass@host:5432/chronos?sslmode=require")
    if err != nil {
        log.Fatal(err)
    }
    defer store.Close()

    if err := store.Migrate(context.Background()); err != nil {
        log.Fatal(err)
    }
}
```

## VectorStore Interface

Vector stores power the knowledge/RAG system. They store and search high-dimensional embeddings.

```go
type VectorStore interface {
    Upsert(ctx context.Context, collection string, embeddings []Embedding) error
    Search(ctx context.Context, collection string, query []float32, topK int, opts ...SearchOption) ([]SearchResult, error)
    Delete(ctx context.Context, collection string, ids []string) error
    CreateCollection(ctx context.Context, name string, dimension int) error
    Close() error
}
```

`Search`'s variadic `SearchOption`s let you scope a shared collection — e.g.
`storage.WithFilter(map[string]any{"tenant": "acme"})` restricts results to
embeddings whose metadata matches every given key/value pair.

### Qdrant Example

```go
import (
    "context"
    "log"

    "github.com/spawn08/chronos/storage"
    "github.com/spawn08/chronos/storage/adapters/qdrant"
)

func main() {
    ctx := context.Background()

    vectors := qdrant.New("http://localhost:6333")
    defer vectors.Close()

    if err := vectors.CreateCollection(ctx, "documents", 1536); err != nil {
        log.Fatal(err)
    }

    var embedding []float32 // from your embeddings provider
    if err := vectors.Upsert(ctx, "documents", []storage.Embedding{
        {ID: "doc-1", Vector: embedding, Metadata: map[string]any{"title": "Guide"}},
    }); err != nil {
        log.Fatal(err)
    }

    var queryVector []float32 // from your embeddings provider
    results, err := vectors.Search(ctx, "documents", queryVector, 5)
    if err != nil {
        log.Fatal(err)
    }
    _ = results
}
```

## YAML Configuration

Storage is configured in the agent YAML:

```yaml
storage:
  backend: sqlite     # sqlite, postgres, none
  dsn: chronos.db     # file path or connection string
```

For PostgreSQL:

```yaml
storage:
  backend: postgres
  dsn: ${DATABASE_URL}
```

## Core Data Types

| Type | Purpose | Key Fields |
|------|---------|------------|
| `Session` | Execution session | ID, AgentID, Status, Metadata |
| `MemoryRecord` | Short/long-term memory | AgentID, Kind, Key, Value |
| `AuditLog` | Security event | Actor, Action, Resource, Detail |
| `Trace` | Observability span | Name, Kind, Input, Output, StartedAt, EndedAt |
| `Event` | Append-only ledger | SessionID, SeqNum, Type, Payload |
| `Checkpoint` | Graph state snapshot | RunID, NodeID, State, SeqNum |

## Implementing a Custom Adapter

Create a new package under `storage/adapters/<name>/` that implements all 21 methods of `Storage` (or the 5 methods of `VectorStore`):

```go
package myadapter

import (
    "context"

    "github.com/spawn08/chronos/storage"
)

type Store struct {
    // your connection fields
}

func New(dsn string) (*Store, error) {
    // connect to your backend
    return &Store{}, nil
}

func (s *Store) Migrate(ctx context.Context) error {
    // create tables/collections
    return nil
}

func (s *Store) Close() error {
    // release resources
    return nil
}

// Implement remaining 19 Storage methods...
```
