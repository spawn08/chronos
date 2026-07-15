# Durable Queue — distributed execution plane

Demonstrates `engine/queue`, the durable, distributed execution plane added in
Phase P1. It decouples run **intake** from **execution** so that work survives
process restarts and scales horizontally across workers.

```bash
go run ./examples/durable_queue
```

No API keys or network needed — the "work" is simulated so the example is
self-contained and deterministic to read.

## What it shows

| Capability | In the example |
|------------|----------------|
| **Leased dequeue** | Three workers claim runs under a time-bounded lease. On Postgres this uses `FOR UPDATE SKIP LOCKED` so workers claim disjoint runs concurrently; on SQLite an atomic `UPDATE … RETURNING`. |
| **Durable sleep** | The `report` run yields with `Result{Sleep: 1s}` and is re-delivered later. Sleeps are intentional yields — they do **not** consume the run's error-retry budget. |
| **Park + signal (HITL)** | The `deploy` run parks with `Result{ParkSignal: "approve"}` and only resumes when `q.Signal(...)` delivers the matching signal — the webhook-as-signal pattern for human approval. Signals are durable: one delivered *before* a run parks is retained and consumed on park (no lost-wakeup race). |
| **Orphan recovery** | A `Reaper` re-enqueues runs whose worker died mid-flight once their lease expires (a lost lease counts as one failed attempt). |
| **Admission control** | `Config{MaxDepth, Policy}` bounds intake; past capacity runs are parked (or rejected) instead of unbounded growth. |

## How an Executor drives the queue

A worker calls your `Executor` for each claimed run; the returned `Result`
tells the queue what happens next:

```go
type Result struct {
    Err        error         // failed → retry with backoff until MaxAttempts, then fail
    Sleep      time.Duration // durably reschedule after the delay
    ParkSignal string        // park until this signal is delivered
    Patch      []byte        // replace the run's persisted payload (state across yields)
}
```

The `Patch` field is how state survives a yield: the example stores a `stage`
in the run payload so the re-delivered run knows whether it has already slept or
been approved.

## Production notes

- Swap `queue.DialectSQLite` for `queue.DialectPostgres` and hand it a Postgres
  `*sql.DB` to run many workers across processes/pods.
- Pair with `graph.NewQueuedExecutor` to run full Chronos `StateGraph`s (agents,
  HITL approval nodes) on the queue instead of the simulated work here.
- The idempotent **outbox** (`EnqueueOutbox` / `ClaimOutbox`) delivers external
  side effects exactly once across retries and resumes; poison entries are
  dead-lettered after a configurable attempt cap.

See [`../durable_hitl`](../durable_hitl) for the graph-level human-in-the-loop
flow and [PLAN.md](../../PLAN.md) Phase P1-A for the full design.
