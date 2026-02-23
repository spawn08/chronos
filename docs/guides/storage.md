---
title: "Storage Adapters"
permalink: /guides/storage/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
---

Chronos defines a single `Storage` interface with 18 methods covering sessions, memory, audit logs, traces, events, and checkpoints. All adapters implement the same contract, so you can swap backends with zero code changes.

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
| Redis | Storage | `storage/adapters/redis` | Available |
| MongoDB | Storage | `storage/adapters/mongo` | Available |
| DynamoDB | Storage | `storage/adapters/dynamo` | Available |
| Qdrant | VectorStore | `storage/adapters/qdrant` | Production-ready |
| Pinecone | VectorStore | `storage/adapters/pinecone` | Available |
| Weaviate | VectorStore | `storage/adapters/weaviate` | Available |
| Milvus | VectorStore | `storage/adapters/milvus` | Available |
| Redis Vector | VectorStore | `storage/adapters/redisvector` | Available |

## SQLite (Development)

The default adapter for development and testing. Data is stored in a single file.

```go
import "github.com/chronos-ai/chronos/storage/adapters/sqlite"

store, err := sqlite.New("chronos.db")
if err != nil {
    log.Fatal(err)
}
defer store.Close()

if err := store.Migrate(context.Background()); err != nil {
    log.Fatal(err)
}
```

For in-memory testing, use `":memory:"`:

```go
store, _ := sqlite.New(":memory:")
```

## PostgreSQL (Production)

The recommended adapter for production deployments.

```go
import "github.com/chronos-ai/chronos/storage/adapters/postgres"

store, err := postgres.New("postgres://user:pass@host:5432/chronos?sslmode=require")
if err != nil {
    log.Fatal(err)
}
defer store.Close()

if err := store.Migrate(context.Background()); err != nil {
    log.Fatal(err)
}
```

## VectorStore Interface

Vector stores power the knowledge/RAG system. They store and search high-dimensional embeddings.

```go
type VectorStore interface {
    Upsert(ctx context.Context, collection string, embeddings []Embedding) error
    Search(ctx context.Context, collection string, query []float32, topK int) ([]SearchResult, error)
    Delete(ctx context.Context, collection string, ids []string) error
    CreateCollection(ctx context.Context, name string, dimension int) error
    Close() error
}
```

### Qdrant Example

```go
import "github.com/chronos-ai/chronos/storage/adapters/qdrant"

vectors := qdrant.New("http://localhost:6333")
defer vectors.Close()

vectors.CreateCollection(ctx, "documents", 1536)

vectors.Upsert(ctx, "documents", []storage.Embedding{
    {ID: "doc-1", Vector: embedding, Metadata: map[string]any{"title": "Guide"}},
})

results, _ := vectors.Search(ctx, "documents", queryVector, 5)
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

Create a new package under `storage/adapters/<name>/` that implements all 18 methods of `Storage` (or the 5 methods of `VectorStore`):

```go
package myadapter

import (
    "context"
    "github.com/chronos-ai/chronos/storage"
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

// Implement remaining 16 Storage methods...
```
