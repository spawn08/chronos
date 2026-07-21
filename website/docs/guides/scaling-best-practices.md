---
title: "Scaling & Best Practices"
---

This guide collects patterns for taking a Chronos agent from a prototype to a production, high-throughput deployment. It maps each recommendation to the concrete Chronos feature that implements it, with links to the relevant guide.

Agentic workloads differ from request/response services in three ways that shape every scaling decision:

- **Long-running & resumable** — a single run may span many LLM calls, tool executions, and human approvals.
- **Expensive & rate-limited** — LLM calls dominate latency and cost, and providers throttle you.
- **Non-deterministic** — the same input can take different paths, so observability and guardrails matter more.

---

## 1. Make execution durable, not in-memory

The single most important scaling decision is to **persist state** so a run can survive a crash, deploy, or scale-down event.

- Model long workflows as a [StateGraph](/guides/stategraph) and enable checkpointing. Each node boundary writes a checkpoint; a new process can `Resume` from the last one.
- Use interrupt nodes for [human-in-the-loop](/guides/stategraph) steps so the process can exit entirely while waiting for approval and resume later.
- Keep node functions **idempotent** where possible — replay and fork (time-travel) re-execute from a checkpoint, and non-deterministic side effects should tolerate being retried.

```go
// A crashed or redeployed worker resumes exactly where it left off.
runner := graph.NewRunner(compiled, store)
state, err := runner.Resume(ctx, sessionID)
```

**Why:** durability lets you run stateless, horizontally-scaled workers behind a queue — any worker can pick up any session because the state lives in shared storage, not process memory.

### The durable work queue (`engine/queue`)

Chronos ships a durable, distributed execution plane that decouples run **intake** from **execution**, so a crash never strands in-flight work:

- **Leased dequeue** — workers claim runs under a time-bounded lease. On PostgreSQL this uses `FOR UPDATE SKIP LOCKED` so many workers claim disjoint runs concurrently; SQLite uses an atomic `UPDATE … RETURNING` for dev/test.
- **Heartbeat + orphan recovery** — a worker heartbeats to hold its lease; a `Reaper` re-enqueues runs whose worker died once the lease expires.
- **Durable sleep & signals** — a run can durably "sleep N, then continue" or **park** until an external signal arrives (the webhook-as-signal pattern for human approval). Signals are retained if delivered before a run parks, so there is no lost-wakeup race. Sleeps and parks do **not** consume the run's error-retry budget.
- **Idempotent outbox** — external side effects are recorded transactionally and delivered exactly once across retries and resumes, with dead-lettering for poison entries.
- **Admission control** — bound queue depth and reject or park work under overload instead of growing unbounded.

Wrap the graph runner with `graph.NewQueuedExecutor` to run full StateGraphs on the queue, or drive the queue directly:

```go
store := queue.NewSQLStore(db, queue.DialectPostgres) // FOR UPDATE SKIP LOCKED
q := queue.New(store, queue.Config{MaxDepth: 1000, Policy: queue.PolicyPark})
_ = q.Migrate(ctx)

// Any number of workers, on any number of hosts, share the queue.
w, _ := queue.NewWorker(q, executor, queue.WorkerConfig{ID: "worker-1", Lease: 30 * time.Second})
go w.Run(ctx)
go queue.NewReaper(q, 5*time.Second).Run(ctx) // recover orphaned runs
```

See the runnable [`durable_queue`](https://github.com/spawn08/chronos/tree/main/examples/durable_queue) example.

---

## 2. Choose storage for your scale

Chronos separates the [`Storage`](/reference/storage) interface (sessions, checkpoints, memory, audit) from the `VectorStore` interface (embeddings). Pick backends per environment:

| Environment | Storage | Vector | Notes |
|-------------|---------|--------|-------|
| Local / dev / tests | SQLite (`:memory:` or file) | in-memory | Zero setup |
| Single production node | SQLite (WAL) or PostgreSQL | Qdrant | Simple, durable |
| Horizontally scaled | **PostgreSQL** | Qdrant / Pinecone / Weaviate / Milvus | Shared state across workers |
| High-throughput cache/session | Redis | — | Fast session/checkpoint access |

Best practices:

- Prefer **PostgreSQL** once you run more than one worker — SQLite doesn't share across hosts.
- Always call `Migrate(ctx)` on startup (or run migrations as a deploy step).
- Keep the vector store separate from relational storage; they scale independently.

See [Storage Adapters](/guides/storage) for configuration.

---

## 3. Control the context window

Unbounded conversation history is the most common cause of runaway cost and latency, and eventually hard failures when you exceed the model's window.

- Enable [context management](/guides/context-management): set `max_tokens`, a `summarize_threshold` (e.g. `0.8`), and `preserve_recent_turns`. Chronos rolls older turns into a summary automatically.
- Cap replayed history with `num_history_runs` so each run only rehydrates what it needs.
- Store durable facts in [long-term memory](/guides/memory) instead of leaving them in the transcript — retrieve them on demand.

```yaml
context:
  max_tokens: 128000
  summarize_threshold: 0.8
  preserve_recent_turns: 5
num_history_runs: 3
```

---

## 4. Cut cost and latency with middleware

Chronos ships composable [hooks](/guides/hooks) — wire them once and every call benefits. See also [Cost Tracking](/guides/cost-tracking).

| Concern | Hook | Effect |
|---------|------|--------|
| Repeated prompts | `CacheHook` | Serve identical LLM calls from cache (TTL + max entries) |
| Provider throttling | `RateLimitHook` | Token-bucket limiting to stay under provider quotas |
| Transient failures | `RetryHook` | Exponential backoff on 429/5xx |
| Budget control | `CostTracker` | Per-model pricing, token accounting, hard budget limits |
| Observability | `MetricsHook` / `LoggingHook` | Latency, call counts, structured events |

Additional resilience:

- Wrap providers in a [fallback chain](/guides/models) (`model.NewFallbackProvider`) so a primary outage degrades to a secondary or local model instead of failing.
- Prefer smaller/cheaper models for routing and classification; reserve the expensive model for the final reasoning step (a `ReasoningModel` can be set separately).

---

## 5. Scale work with teams and parallelism

- Use [multi-agent teams](/guides/teams) to decompose work: **router** to dispatch, **parallel** to fan out independent subtasks, **sequential** for pipelines, **coordinator** for delegation.
- For parallel strategies, bound concurrency with `max_concurrency` so you don't exhaust provider rate limits or memory.
- Choose an `error_strategy` (`fail_fast`, `collect`, `best_effort`) that matches whether partial results are acceptable.
- Within a graph, use fan-out/fan-in nodes with a `MergeFunc` for concurrent branches that rejoin.

---

## 6. Isolate untrusted execution

When agents run generated code or shell commands, contain them:

- Use the [process sandbox](/guides/examples/durability#sandbox_execution) (`sandbox.NewProcessSandbox`) with a scoped working directory, timeouts, and captured output.
- Gate dangerous tools behind approval: set `Permission: tool.PermRequireApproval` (or `PermDeny`) in the [tool registry](/guides/tools).
- Apply [guardrails](/guides/guardrails) on inputs and outputs (blocklists, length limits, schema validation) to catch prompt injection and malformed output early.

---

## 7. Observe everything

Non-determinism makes observability non-optional.

- Stream execution events via the [SSE broker](/guides/streaming) to dashboards and clients.
- Emit custom events from nodes/tools (`stream.Emit`) for domain-specific telemetry.
- Run behind [ChronosOS](/reference/architecture) for centralized tracing, audit logs, and approval enforcement.
- Track spend continuously with `CostTracker` and alert on budget thresholds before they're hit.

---

## 8. Deploy for horizontal scale

- Run **stateless workers**: all durable state lives in Postgres/Redis + the vector store, so you can scale replicas up and down freely. See [Docker](/deployment/docker) and [Kubernetes & Helm](/deployment/kubernetes).
- Use the Helm chart's HPA to autoscale on CPU/queue depth; keep readiness gated on storage connectivity.
- Inject secrets (API keys, DSNs) via env vars and `${VAR}` expansion in YAML — never commit keys. See [Configuration](/getting-started/configuration).
- Set provider `timeout_sec` and rely on `RetryHook` + fallback providers so a slow upstream doesn't pin a worker.
- Wire [CI/CD](/deployment/cicd) to run `go build ./...`, `go vet`, and tests before deploy.

---

## Checklist

Before going to production, confirm you have:

- [ ] Durable storage (PostgreSQL for multi-worker) with `Migrate` on startup
- [ ] Checkpointing enabled for long/interruptible workflows
- [ ] Context management configured (summarization + `num_history_runs`)
- [ ] `CostTracker` with a budget limit and alerting
- [ ] `RetryHook` + a fallback provider chain
- [ ] `RateLimitHook` sized to your provider quotas
- [ ] `CacheHook` for repeatable prompts
- [ ] Guardrails on inputs and outputs
- [ ] Sandbox + approval for code/shell tools
- [ ] Streaming/tracing wired to a dashboard
- [ ] Secrets via env vars, not in config files
- [ ] Stateless workers behind an autoscaler

## See also

- [Building Real-World Agents](/guides/real-world-agents) — an end-to-end walkthrough
- [Context Management](/guides/context-management) · [Cost Tracking](/guides/cost-tracking) · [Hooks](/guides/hooks)
- [Kubernetes & Helm](/deployment/kubernetes) · [Architecture](/reference/architecture)
