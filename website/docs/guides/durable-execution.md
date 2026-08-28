---
title: "Durable Execution"
---

Chronos runs agents on a **durable, distributed execution plane** (`engine/queue`)
that decouples run *intake* from *execution*. Work is persisted before it runs,
so a crash, deploy, or scale-down never loses or strands it — any worker on any
replica can pick up any run.

See the runnable [`durable_queue`](https://github.com/spawn08/chronos/tree/main/examples/durable_queue) example.

## Why a queue

The graph runner alone executes synchronously in the caller's goroutine — fine
for a request, but a long agent run (many LLM calls, tool executions, human
approvals) that dies mid-flight is lost. The queue makes execution:

- **Durable** — state lives in shared storage, not process memory.
- **Distributed** — N stateless workers across N pods share one queue.
- **Recoverable** — a dead worker's in-flight run is re-enqueued automatically.

## Core concepts

| Concept | What it does |
|---------|--------------|
| **Run** | One unit of durable work (a graph execution or resume), with a payload, priority, and retry budget. |
| **Leased dequeue** | A worker claims a run under a time-bounded lease. On Postgres this uses `FOR UPDATE SKIP LOCKED` so many workers claim disjoint runs concurrently; SQLite uses an atomic `UPDATE … RETURNING`. |
| **Heartbeat** | The worker extends its lease while executing; if it stops, the lease expires. |
| **Reaper** | Re-enqueues runs whose lease expired (worker died). A lost lease counts as one failed attempt. |
| **Durable sleep** | A run yields with `Result{Sleep: d}` and is re-delivered after the delay — "wait N, then continue" that survives restarts. Sleeps do **not** consume the retry budget. |
| **Park + signal** | A run parks with `Result{ParkSignal: name}` and resumes only when `Signal(...)` delivers the matching signal — the webhook-as-signal pattern for human approval. Signals delivered before a run parks are retained (no lost-wakeup race). |
| **Outbox** | External side effects recorded transactionally and delivered exactly once across retries/resumes; poison entries are dead-lettered after a cap. |
| **Admission control** | `MaxDepth` + `Policy` (reject or park) bounds intake so overload sheds gracefully instead of growing unbounded. |

## Setting it up

```go
import (
    "context"
    "database/sql"
    "log"

    _ "modernc.org/sqlite" // pure-Go driver; swap for a Postgres driver in production

    "github.com/spawn08/chronos/engine/queue"
)

ctx := context.Background()

// This example uses SQLite (queue.DialectSQLite) so it is runnable with no
// external services; production uses Postgres (queue.DialectPostgres) so
// dequeue can use FOR UPDATE SKIP LOCKED across replicas.
db, err := sql.Open("sqlite", "queue.db")
if err != nil {
    log.Fatal(err)
}
store := queue.NewSQLStore(db, queue.DialectSQLite)
q := queue.New(store, queue.Config{
    MaxDepth:           1000,
    Policy:             queue.PolicyPark, // park (not reject) under overload
    DefaultMaxAttempts: 3,                // error-retry budget per run
})
if err := q.Migrate(ctx); err != nil {
    log.Fatal(err)
}
```

### Workers and the reaper

Continuing with the `q` and `ctx` constructed above (this snippet additionally
needs `"time"` and an `executor` of type `queue.Executor`, shown next):

```go
// Run as many workers as you like, on as many hosts as you like.
w, _ := queue.NewWorker(q, executor, queue.WorkerConfig{
    ID:        "worker-1",
    Lease:     30 * time.Second,
    Heartbeat: 10 * time.Second,
})
go w.Run(ctx)

// One or more reapers recover orphaned runs.
go queue.NewReaper(q, 5*time.Second).Run(ctx)
```

### The executor

A worker calls your `Executor` for each claimed run; the returned `Result` tells
the queue what happens next:

```go
type Result struct {
    Err        error         // failed → retry with backoff until MaxAttempts, then fail
    Sleep      time.Duration // durably reschedule after the delay
    ParkSignal string        // park until this signal is delivered
    Patch      []byte        // replace the run's persisted payload (state across yields)
}
```

`Patch` is how state survives a yield: store progress in the run payload so the
re-delivered run knows where it left off.

## Running full graphs on the queue

Wrap the graph runner with `graph.NewQueuedExecutor` to execute complete Chronos
`StateGraph`s — including human-in-the-loop approval nodes — on the queue instead
of synchronously:

```go
import "github.com/spawn08/chronos/engine/graph"

// checkpointStore is a storage.Storage (e.g. storage/adapters/sqlite or
// storage/adapters/postgres) that holds graph checkpoints. It is distinct
// from the queue.SQLStore constructed above, which persists the work queue
// itself — the two stores may share a *sql.DB but implement different
// interfaces (storage.Storage vs. queue.Store).
//
// resolver maps a run's GraphID to its CompiledGraph; SingleGraphResolver is
// the simplest resolver when a worker only ever executes one graph.
resolver := graph.SingleGraphResolver(compiledGraph)
qe := graph.NewQueuedExecutor(checkpointStore, resolver)
w, _ := queue.NewWorker(q, qe.Executor(), queue.WorkerConfig{ID: "w1", Lease: 30 * time.Second})
go w.Run(ctx)
```

A parked HITL run resumes when a webhook delivers `graph.ApprovalSignal(sessionID)`.

## Retry budget vs. yields

`MaxAttempts` is the number of *failed* attempts allowed. Only genuine failures
(an executor returning `Err`, or a lease lost to a dead worker) consume it —
durable sleeps and parks are intentional yields and never burn the budget, so a
long-running or approval-gated workflow is never terminally failed just for
waiting.

## SQLite vs. Postgres

| | SQLite | Postgres |
|--|--------|----------|
| Use | dev, tests, single node | production, multi-replica |
| Dequeue | atomic `UPDATE … RETURNING` (serialized writers) | `FOR UPDATE SKIP LOCKED` (concurrent claims) |
| Dialect | `queue.DialectSQLite` | `queue.DialectPostgres` |

The queue owns its own schema (`Migrate`) over any `*sql.DB` and never touches
the shared `storage` tables.

## See also

- [Scaling & Best Practices](/guides/scaling-best-practices) — where durable execution fits in the broader scaling picture.
- [StateGraph](/guides/stategraph) — checkpointing and human-in-the-loop nodes.
