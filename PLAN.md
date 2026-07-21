# Chronos — Production & High-Scale Readiness Plan

> Source of truth for forward-looking work. Derived from a full production-readiness
> review of the codebase (engine, SDK, storage, control plane, sandbox, deploy, CI).
> Items are grouped into three phases. **Do P0 before shipping to any production user.**
> Each item lists the problem, location, action, and acceptance criteria.

## Build-out roadmap (complete)

The original seven-tier build-out delivered all 101 tracked items:

| Priority | Items | Status |
|----------|-------|--------|
| P0 — Critical bugs & wiring fixes | 16 | Complete |
| P1 — Core feature gaps (parity)   | 28 | Complete |
| P2 — Ecosystem & developer experience | 30 | Complete |
| P3 — Ecosystem expansion          | 27 | Complete |

**P0:** Redis list methods, RedisVector search, RetryHook retries, NumHistoryRuns,
OutputSchema validation, Runner-SSE events, trace collector wiring, CLI commands, tests.
**P1:** MCP (stdio), subgraphs, time travel, streaming, HITL, context management, auth,
evaluation framework, in-memory adapter, health endpoints.
**P2:** Built-in toolkits, doc loaders, multimodal, functional graph API, visualization,
observability, scheduler, guardrails.
**P3:** Bedrock/Cohere providers, pgvector/pinecone/weaviate/milvus/chromadb/lancedb
vector stores, swarm/hierarchy multi-agent, reasoning strategies, sandbox backends,
migration framework.

Several items are functional but prototype-grade — correctness and scale gaps are
captured below.

## How to use this file

- Phases run in order: **P0 (correctness & safety) → P1 (scale foundations) → P2 (hardening & platform)**.
- Within a phase, workstreams are largely independent and can be parallelized.
- Mark an item `[x]` when its acceptance criteria are met; append `<!-- done: YYYY-MM-DD -->`.
- Every fix must ship with a table-driven test; concurrency fixes must ship with a `-race` stress test.

## Maturity snapshot (why this plan exists)

| Dimension | Rating | Note |
|-----------|:------:|------|
| API / interface design | 8/10 | Clean seams; most fixes are additive. |
| Feature breadth | 8/10 | Broad, but much is prototype-grade. |
| Correctness | 4/10 | ~10 day-one bugs, some disable flagship features. |
| Scale readiness | 3/10 | No durable/distributed execution plane. |
| Security posture | 3/10 | Auth written but unwired; unsafe default sandbox. |

---

# Phase P0 — Correctness & Safety (blocks production)

Fix broken behavior in shipped code. Target: a small number of weeks.

> **Status: COMPLETE** <!-- done: 2026-07-15 --> — delivered in wave 1 (PR #17, branch `plan/p0-wave1`).
> Every item ships with a regression test (`runner_p0_test.go`, `registry_safety_test.go`,
> `server_security_test.go`, `agent_toolloop_p0007_test.go`, `isolation_p0008_test.go`).

## P0-A: Durable execution correctness (`engine/graph`)

- [x] **P0-001 — HITL resume infinite-pause.** Resuming a paused run restores `CurrentNode`
  to the interrupt node, which immediately re-pauses. There is no "approved once" flag.
  - **Location:** `engine/graph/runner.go:81-99,205-229`
  - **Action:** Add a resume token / `resumedFrom` marker (or per-node `approved` flag in `RunState`)
    so `Resume` advances *past* the interrupt node exactly once, then clears the marker.
  - **Done when:** A paused approval workflow resumes and completes; regression test covers pause → resume → completion.

- [x] **P0-002 — Resume double-executes the last node.** The checkpoint records the just-finished
  node (before `findNext`), so `Resume` re-runs it, duplicating side effects.
  - **Location:** `engine/graph/runner.go:278-301,324-335`
  - **Action:** Checkpoint the *next* node to execute (post-`findNext`), or record a
    "node N completed" high-water mark and skip completed nodes on resume.
  - **Done when:** Resume continues from the next unexecuted node; a side-effect counter test proves no double-execution.

- [x] **P0-003 — `GetLatestCheckpoint` orders by wall-clock.** Same-tick timestamps make resume
  restore a non-deterministic (possibly earlier) checkpoint.
  - **Location:** `storage/adapters/sqlite/sqlite.go:341`, `storage/adapters/postgres/postgres.go:343`
  - **Action:** `ORDER BY seq_num DESC LIMIT 1`.
  - **Done when:** Latest checkpoint is deterministic; test with equal timestamps.

- [x] **P0-004 — Non-atomic, non-idempotent checkpoint + event write.** `SaveCheckpoint` and
  `AppendEvent` are separate calls, the event error is discarded, and `SaveCheckpoint` is a plain
  `INSERT`. Ledger can silently gap; replay errors on PK.
  - **Location:** `engine/graph/runner.go:279-291`; adapter `SaveCheckpoint`/`AppendEvent`
  - **Action:** Wrap both in one transaction; make `SaveCheckpoint` an upsert (`ON CONFLICT`);
    add a unique `(session_id, seq_num)` constraint; stop discarding the append error.
  - **Done when:** Crash-between-writes cannot desync ledger and checkpoint; replay is idempotent.

- [x] **P0-005 — Runner channel leak / reuse panic.** `close(localCh)` only runs on the clean exit
  path; error/interrupt paths leak it, and reusing a Runner sends/closes on a closed channel → panic.
  - **Location:** `engine/graph/runner.go:48-60,205-309`
  - **Action:** Close `localCh` on all exit paths (defer at top of `execute`); guard `emit` against a
    closed channel or make Runner explicitly single-use with a state check.
  - **Done when:** No goroutine leak on error/pause; misuse returns an error, never panics.

- [x] **P0-006 — No step limit / cycle guard.** `for rs.Status == RunStatusRunning` can loop forever,
  writing a checkpoint per iteration.
  - **Location:** `engine/graph/runner.go:205`
  - **Action:** Add a configurable max-step bound and per-node timeout; fail the run when exceeded.
  - **Done when:** A cyclic graph terminates with a clear error; no unbounded checkpoint growth.

## P0-B: Agent-loop & memory correctness (`sdk/`)

- [x] **P0-007 — Multi-round tool context is dropped.** `handleToolCalls` receives `messages` by
  value and appends locally, so prior rounds' tool calls/results are discarded; the follow-up
  `Chat` also passes no `Tools`.
  - **Location:** `sdk/agent/agent.go:390-405,430-489`
  - **Action:** Thread the message slice by pointer (or return the accumulated messages) so each
    round sees full history; pass `req.Tools` on every follow-up call; surface an error when
    `MaxIterations` is hit with unsatisfied tool calls.
  - **Done when:** A 3-round sequential-tool task retains full context; test asserts message accumulation.

- [x] **P0-008 — Multi-tenant memory leak.** `Manager.userID` is ignored; long-term memory is keyed
  by `agentID` only, so `GetUserMemories` returns every user's memories.
  - **Location:** `sdk/memory/memory.go:37-64`, `sdk/memory/manager.go:119-133`
  - **Action:** Key long-term memory by `(agentID, userID)`; scope all reads by `userID`.
  - **Done when:** Two users on one agent never see each other's memories; test proves isolation.

- [x] **P0-009 — No panic recovery anywhere.** A panicking tool / node / MCP handler crashes the
  whole process.
  - **Location:** `engine/tool/registry.go:136`, `engine/graph/runner.go:254`, `sdk/protocol/protocol.go:463-496`
  - **Action:** `recover()` around user-supplied handlers; convert panics to errors and emit an event/trace.
  - **Done when:** A panicking tool fails only its call; the server stays up; test covers it.

## P0-C: Control-plane baseline security (`os/`)

- [x] **P0-010 — All endpoints unauthenticated.** Auth/CORS/rate-limit middleware exists but is
  never attached, including state-mutating `POST /api/sessions/state`.
  - **Location:** `os/server.go:62-83,222-225`; `os/auth/*`, `os/middleware/*`
  - **Action:** Compose a middleware chain (auth → CORS → rate limit → recovery → logging) around the mux.
  - **Done when:** Protected routes reject unauthenticated requests; integration test covers 401/200 paths.

- [x] **P0-011 — No server hardening.** Missing `Read/Write/Idle/ReadHeader` timeouts, body size
  limits, and panic-recovery middleware; readiness runs `Migrate()` on every probe.
  - **Location:** `os/server.go:104,222-225`
  - **Action:** Set `http.Server` timeouts; wrap bodies with `http.MaxBytesReader`; add recovery
    middleware; move migration out of the readiness handler (run at startup / via CLI).
  - **Done when:** Slowloris and oversized-body requests are rejected; readiness is a cheap check.

- [x] **P0-012 — Postgres adapter is dead code.** No Postgres driver is imported anywhere, so the
  "production" store fails on first query.
  - **Location:** `storage/adapters/postgres/postgres.go:21`, `go.mod`
  - **Action:** Add `jackc/pgx` (or `lib/pq`) and register it; wire it into `sdk/agent/config.go` `buildStorage`.
  - **Done when:** `postgres.New` connects and passes the SQLite test suite against a real Postgres (testcontainers).

---

# Phase P1 — Scale Foundations

Make the platform horizontally scalable and resilient. Target: 1–2 quarters.

> **Wave 2 status** <!-- done: 2026-07-15 --> — P1-A, P1-B, P1-C, P1-E delivered on
> branch `plan/p1-wave2` (new `engine/queue/` durable execution plane, model/LLMOps
> hardening, storage scale plane, MCP reliability). Two adversarial review agents
> (design + code-quality) gated the work; their findings were fixed in-branch
> (queue retry-budget/park-race/outbox dead-letter, ctx-aware retry, enforced
> token rate limit, batch param-limit chunking, instant-correct retention, atomic
> rollback). Full `-race` suite green; full-repo golangci-lint clean.
> **P1-D (below) is deferred to Wave 3** — it depends on the durable queue landing first.

## P1-A: Durable & distributed execution (highest-leverage)

- [x] **P1-001 — Durable work queue with leased dequeue.** Today the graph runner executes
  synchronously in the caller's goroutine; a crash strands in-flight runs.
  - **Action:** Introduce a `runs` queue backed by Postgres (`FOR UPDATE SKIP LOCKED`) or NATS/Redis
    Streams; workers claim runs with a lease and execute the graph; decouple intake from execution.
  - **Done when:** A run submitted on node A can be executed by worker B; killing a worker mid-run
    lets another worker resume it.

- [x] **P1-002 — Heartbeat, lease expiry & orphan recovery.** Detect dead workers and re-enqueue
  their in-flight runs.
  - **Done when:** A `SIGKILL`ed worker's run is picked up and completed by another within the lease TTL.

- [x] **P1-003 — Idempotency keys + outbox.** Combine with P0-004 so checkpoint + event + external
  effect converge without duplicates on retry/resume.
  - **Done when:** Replaying a resumed run does not double-emit external effects; outbox drains reliably.

- [x] **P1-004 — Durable timers/sleeps and external signals.** Enable "wait N, then continue",
  scheduled continuations, and webhook-as-signal for HITL.
  - **Done when:** A graph can durably sleep across a process restart and resume on a signal.

- [x] **P1-005 — Global admission control / back-pressure.** Bound queue depth and reject/park
  work under overload instead of unbounded synchronous execution.
  - **Done when:** Load test shows graceful shedding, not OOM, past capacity.

## P1-B: Model serving & LLMOps (`engine/model`, `engine/hooks`)

- [x] **P1-006 — Tuned HTTP transport.** Default transport keeps only 2 idle conns/host → churn.
  - **Location:** `engine/model/httpclient.go:20-31`
  - **Action:** Custom `http.Transport` with tuned `MaxIdleConns`, `MaxIdleConnsPerHost`,
    `MaxConnsPerHost`, keep-alive; separate connect vs. total/streaming timeouts.

- [x] **P1-007 — Real retry/backoff with 429 handling + circuit breakers.** `MaxRetries` is defined
  but never used; fallback fires on non-retryable errors.
  - **Location:** `engine/model/*.go`, `engine/model/fallback.go`, `engine/hooks/retry.go`
  - **Action:** Classify errors (retryable vs. terminal); exponential backoff honoring `Retry-After`;
    per-provider circuit breaker; make fallback skip terminal (4xx) errors. Fix RetryHook data race
    (shared `Retries` counter) with a mutex.

- [x] **P1-008 — Streaming hardening.** Abandoned streams leak goroutines/connections; 64KB line cap
  silently truncates; `scanner.Err()` unchecked; Anthropic `tool_use` deltas dropped; no streamed usage.
  - **Location:** `engine/model/openai.go`, `anthropic.go`
  - **Action:** `ctx`-aware channel sends; `scanner.Buffer()` override; check `scanner.Err()`; handle
    `tool_use`/`input_json_delta`; request `stream_options.include_usage`.
  - **Done when:** Disconnecting a client frees the goroutine/connection; streamed tool calls and token
    usage are captured.

- [x] **P1-009 — Real tokenizer + token streaming to callers.** Replace the `len/4` heuristic with a
  BPE tokenizer; invoke `StreamChat` from the SDK so callers get token deltas.
  - **Location:** `engine/model/tokenizer.go`, `sdk/agent/agent.go`

- [x] **P1-010 — Fix rate-limit / cache / cost hooks for scale.** RateLimitHook holds a global mutex
  while waiting (serializes all calls); CacheHook is unbounded with O(n) eviction; CostHook is TOCTOU
  and counts unknown models as $0.
  - **Location:** `engine/hooks/{ratelimit,cache,cost}.go`
  - **Action:** Release the lock while waiting; bound cache with proper LRU+TTL; atomic cost accounting;
    reject/flag unknown models.

## P1-C: Storage & data plane

- [x] **P1-011 — Indexes + configurable pooling.** Add indexes on `sessions.agent_id`,
  `audit_logs.session_id`, `traces.session_id`; make pool sizes configurable.
- [x] **P1-012 — Pagination + retention.** `ListTraces/ListEvents/ListCheckpoints` are unbounded and
  never trimmed. Add `limit`/cursor to the interface; add TTL/partitioning/retention jobs.
  - **Location:** `storage/storage.go:98-108`, adapters.
- [x] **P1-013 — Batch ingestion.** Replace per-row loops (vector upserts, events) with batch/`COPY`.
- [x] **P1-014 — Wire the migration framework.** It exists but is orphaned and hardcodes `?` placeholders
  (breaks Postgres). Make adapters use it; support `$N`; add an advisory lock for concurrent migrators.
  - **Location:** `storage/migrate/migrate.go`

## P1-D: Control plane, SSE & observability

> **COMPLETE** <!-- done: 2026-07-15 --> — delivered in Wave 3 (branch `plan/p1-wave3`):
> per-session SSE routing with a firehose subscriber; queue/SKIP-LOCKED scheduler with
> exactly-once claim; pluggable store-backed rate limiter; ctx-aware persisted approval
> wired to the tool path; metrics fed from the execution path (hook bridge) + OTLP export
> + structured logging with correlation ids + per-tenant attribution; protocol bus
> correlation-map reply routing + bounded handler pool. Two adversarial review agents gated
> it; findings fixed in-branch (approval decision divergence, SSE custom-event leak, rate-limit
> read-error corruption, metrics key collision, engine→os tracer layer breach). Full `-race`
> green; golangci-lint clean.

- [x] **P1-015 — SSE topic/session routing.** The Broker broadcasts every event to every subscriber
  (cross-session/tenant leakage) and `SSEHandler` uses a static id so clients clobber each other.
  - **Location:** `engine/stream/stream.go:44-88`
  - **Action:** Key subscriptions per session/tenant with unique ids; add heartbeat/keepalive; cap
    subscribers; externalize fan-out (Redis/NATS) for multi-replica.

- [x] **P1-016 — Externalize control-plane state.** Scheduler, rate limiter, and approval live in
  in-process maps (never started / duplicate-fire across replicas / block forever).
  - **Location:** `os/scheduler/*`, `os/middleware/ratelimit.go`, `os/approval/approval.go`, `os/server.go:47-54`
  - **Action:** Back scheduler with the durable queue + leader election (or `SKIP LOCKED`); distributed
    rate limiter; ctx-aware, persisted, authorized approval service wired to the tool path (fix the
    `ApprovalFunc` signature mismatch and ID-collision bug).

- [x] **P1-017 — Real observability.** Feed the (currently never-incremented, histogram-buggy) metrics
  from the execution path; emit OpenTelemetry metrics + real OTLP trace export; structured JSON logging
  with correlation IDs; per-tenant cost/latency/token attribution.
  - **Location:** `os/metrics/prometheus.go`, `os/trace/otel.go`

- [x] **P1-018 — Protocol bus correctness.** Fix the reply-routing race (concurrent `SendAndWait` on a
  shared sender inbox mis-delivers replies) with a correlation map keyed by message id; bound
  handler-goroutine spawning (real back-pressure); propagate caller `ctx` into handlers; correct the
  "lock-free / object-pooling" claims or make them true.
  - **Location:** `sdk/protocol/protocol.go`

## P1-E: MCP reliability

- [x] **P1-019 — MCP per-call timeout & deadlock.** `callLocked` ignores the per-call ctx and blocks on
  an unbounded read while holding the mutex; `Close` can't recover a hung client; SSE transport is a stub;
  MCP tools register with no `Permission` (auto-allow).
  - **Location:** `engine/mcp/client.go`, `engine/mcp/adapter.go`
  - **Action:** Honor per-call ctx with read deadlines; bound read size; make `Close` force-kill the
    subprocess; default MCP tools to `PermRequireApproval`.

---

# Phase P2 — Hardening & Platform

Sustained-scale operability and security. Ongoing.

- [x] **P2-001 — Sandbox hardening.** Default sandbox is bare `exec` (no isolation) and
  `NewAutoShellTool` runs `sh -c` on the host auto-approved; the container backend runs as root with no
  seccomp/pids limit; `wasm`/`k8s` backends are stubs that fail at execution time.
  - **Location:** `sandbox/sandbox.go`, `sandbox/container.go`, `engine/tool/builtins/shell.go`, `sandbox/{wasm,k8sjob}.go`
  - **Action:** Add gVisor/Kata/Firecracker; hardened container profile (non-root, seccomp, `CapDrop`,
    `no-new-privileges`, pids/ulimit/tmpfs caps); remove or gate the auto-allow host-shell tool; make
    stub backends fail at construction, not execution.

- [ ] **P2-002 — Multi-tenancy in the data model.** No tenant/org column exists anywhere; isolation is
  currently impossible. Add tenant scoping to schema + all queries; enforce object-level authorization
  (session/trace/checkpoint access is keyed only by client-supplied IDs today → IDOR).
  - **Location:** `storage/storage.go`, `os/server.go:118-217`

- [x] **P2-003 — AuthN/Z depth.** Current JWT is HS256-only with unused `Issuer`/`AllowExpired`; API keys
  are plaintext in memory. Add OIDC/JWKS + RS256 + rotation; hashed, persisted API keys; per-key/per-tenant
  quotas and token/cost budgets.
  - **Location:** `os/auth/{jwt,apikey}.go`

- [x] **P2-004 — Production Helm chart.** Missing liveness/readiness probes (endpoints exist!), PDB,
  `securityContext`, anti-affinity/topology spread, ServiceMonitor; toy resource limits; autoscaling & TLS
  off by default; secrets as `b64enc` with a hardcoded `changeme` DSN.
  - **Location:** `deploy/helm/chronos/`
  - **Action:** Add probes/PDB/securityContext/anti-affinity/ServiceMonitor; realistic resources; enable
    HPA; External-Secrets/SOPS/Vault integration; TLS via Ingress.

- [x] **P2-005 — CI supply-chain security.** No `govulncheck`, image scanning (Trivy/Grype), SAST
  (CodeQL/gosec), SBOM, or image signing (cosign). Add all; enforce (not warn) a meaningful coverage gate.
  - **Location:** `.github/workflows/ci.yml`, `release.yml`

- [ ] **P2-006 — Test quality.** ~110 of 263 test files are coverage-padding (`_squeeze/_boost/_max`),
  there are zero benchmarks, and no load/soak/chaos tests. Add benchmarks for hot paths; concurrency
  stress tests under `-race` for `sdk/protocol`, `sdk/team`, `engine/hooks`, `engine/graph`; load and
  soak tests for the durable queue and control plane.

- [ ] **P2-007 — Storage adapter quality.** dynamo/mongo/redis/redisvector are prototype REST/TCP glue
  with wire-protocol bugs (e.g. single-64KB `Read` RESP parsing, missing SigV4). Rebuild on official SDKs
  or clearly label experimental and exclude from the "production" set.
  - **Location:** `storage/adapters/{dynamo,mongo,redis,redisvector}/`

- [x] **P2-008 — Config-driven completeness.** YAML custom tools are placeholders and config-driven
  Postgres is a stub. Make config-built agents fully functional (custom tool handlers, Postgres).
  - **Location:** `sdk/agent/config.go:368-418`

- [x] **P2-009 — RAG/knowledge scaling.** `Load` embeds all docs in one call (fails on large corpora),
  there's no chunking in the indexing path, no query-embedding cache, and no top-k/score threshold on
  retrieval. Add batching, chunking, caching, and relevance thresholds; guard concurrent indexing.
  - **Location:** `sdk/knowledge/vectordb.go`

---

## Dependencies (critical path)

```
P0 (correctness/safety) ──► P1-A durable execution ──► P1-D externalized control plane ──► P2 platform
        │                          │
        └─ P0-012 pg driver ──► P1-C storage plane ──► P2-002 multi-tenancy
        └─ P0-009 recover ──────────────────────────► P2-001 sandbox hardening
```

## Definition of "production-ready at high scale"

1. P0 complete — no known correctness bug, auth on, safe sandbox by default.
2. P1-A/P1-D complete — kill any pod and lose no work; run N replicas with no duplicate/leaked behavior.
3. Load + soak + chaos suite green; per-tenant isolation, quotas, and observability in place.
