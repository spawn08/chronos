# Chronos — Production & High-Scale Readiness Plan

> Historical record of the production-readiness work: a full review of the codebase (engine,
> SDK, storage, control plane, sandbox, deploy, CI) turned into three phases (P0 correctness →
> P1 scale foundations → P2 hardening/platform). **All phases are complete.** Item-level detail
> (problem/location/action/acceptance criteria) that used to live here has been condensed —
> full history is in the branches referenced below and in `git log`. One follow-up from P2-002
> remains genuinely open; see "Known follow-ups" below.
>
> Forward-looking capability work now lives under [`plan/`](plan/README.md) — see "Next: V2" below.

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

---

## Phase P0 — Correctness & Safety — COMPLETE <!-- done: 2026-07-15 -->

Delivered on `plan/p0-wave1` (PR #17). Every item shipped with a regression test
(`runner_p0_test.go`, `registry_safety_test.go`, `server_security_test.go`,
`agent_toolloop_p0007_test.go`, `isolation_p0008_test.go`).

| Item | Fixed |
|------|-------|
| P0-001 | HITL resume infinite-pause — resume advances past the interrupt node exactly once. |
| P0-002 | Resume double-executing the last node — checkpoint now records the *next* node. |
| P0-003 | `GetLatestCheckpoint` non-determinism — ordered by `seq_num`, not wall-clock. |
| P0-004 | Non-atomic checkpoint+event write — single transaction, upsert, unique constraint. |
| P0-005 | Runner channel leak / reuse panic — closed on all exit paths, `emit` guarded. |
| P0-006 | No step limit / cycle guard — configurable max-step bound + per-node timeout. |
| P0-007 | Multi-round tool context dropped — message history threaded correctly across rounds. |
| P0-008 | Multi-tenant memory leak — long-term memory keyed by `(agentID, userID)`. |
| P0-009 | No panic recovery — `recover()` around tool/node/MCP handlers. |
| P0-010 | All endpoints unauthenticated — auth/CORS/rate-limit/recovery chain wired to the mux. |
| P0-011 | No server hardening — timeouts, body-size limits, recovery middleware. |
| P0-012 | Postgres adapter was dead code — driver wired and registered. |

## Phase P1 — Scale Foundations — COMPLETE <!-- done: 2026-07-15 -->

Delivered on `plan/p1-wave2` (P1-A/B/C/E) and `plan/p1-wave3` (P1-D). Two adversarial review
agents (design + code-quality) gated all of it; findings fixed in-branch. Full `-race` suite
green; full-repo `golangci-lint` clean.

| Item | Fixed |
|------|-------|
| P1-001 | Durable work queue with leased dequeue (`engine/queue/`, `SKIP LOCKED`). |
| P1-002 | Heartbeat, lease expiry & orphan recovery for dead workers. |
| P1-003 | Idempotency keys + outbox — no duplicate effects on retry/resume. |
| P1-004 | Durable timers/sleeps + external signals across process restarts. |
| P1-005 | Global admission control / back-pressure — graceful shedding under overload. |
| P1-006 | Tuned HTTP transport (idle conns, keep-alive, connect vs. total timeouts). |
| P1-007 | Real retry/backoff with 429 handling + circuit breakers; RetryHook race fixed. |
| P1-008 | Streaming hardening — ctx-aware sends, unbounded scanner cap, `tool_use` deltas, usage. |
| P1-009 | Real BPE tokenizer + token streaming to callers. |
| P1-010 | Rate-limit/cache/cost hooks fixed for scale (no lock-while-waiting, bounded LRU+TTL). |
| P1-011 | Indexes on hot columns + configurable connection pooling. |
| P1-012 | Pagination + retention on `ListTraces/ListEvents/ListCheckpoints`. |
| P1-013 | Batch ingestion (vector upserts, events) replacing per-row loops. |
| P1-014 | Migration framework wired (adapters use it; `$N` support; advisory lock). |
| P1-015 | SSE topic/session routing — per-session subscriptions, heartbeat, externalized fan-out. |
| P1-016 | Externalized control-plane state — durable scheduler, distributed rate limiter, persisted approval. |
| P1-017 | Real observability — metrics fed from execution path, OTLP export, per-tenant attribution. |
| P1-018 | Protocol bus correctness — correlation-map reply routing, bounded handler pool. |
| P1-019 | MCP per-call timeout & deadlock fixed; tools default to `PermRequireApproval`. |

## Phase P2 — Hardening & Platform — COMPLETE

| Item | Fixed |
|------|-------|
| P2-001 | Sandbox hardening — hardened container profile; stub backends fail at construction. |
| P2-002 | Multi-tenancy in the data model — `tenant_id` + context propagation, IDOR closed. <!-- done: 2026-07-22 --> ⚠️ see follow-up below |
| P2-003 | AuthN/Z depth — OIDC/JWKS+RS256+rotation, hashed persisted API keys, per-tenant quotas. |
| P2-004 | Production Helm chart — probes/PDB/securityContext/anti-affinity/HPA/TLS. |
| P2-005 | CI supply-chain security — govulncheck, image scanning, SAST, SBOM, cosign signing. |
| P2-006 | Test quality — benchmarks + `-race` stress tests + queue load/soak tests. <!-- done: 2026-07-22 --> |
| P2-007 | Storage adapter quality — dynamo/mongo/redis/redisvector rebuilt on official SDKs. <!-- done: 2026-07-22 --> |
| P2-008 | Config-driven completeness — YAML custom tool handlers + config-driven Postgres. |
| P2-009 | RAG/knowledge scaling — batching/chunking/cache/threshold (later defaulted by `plan/` WC-D-002). |

### Known follow-ups (not fully closed)

- **P2-002 tenant scoping gap:** the four experimental storage adapters (dynamo/mongo/redis/
  redisvector) do not yet enforce tenant scoping — their data lands under the default tenant.
  P2-007 rebuilt these adapters on official SDKs but did not add `TenantFromContext`
  stamping/filtering. Apply it before treating any of the four as tenant-safe for production use.

---

## Definition of "production-ready at high scale"

1. P0 complete — no known correctness bug, auth on, safe sandbox by default. ✅
2. P1-A/P1-D complete — kill any pod and lose no work; run N replicas with no duplicate/leaked behavior. ✅
3. Load + soak + chaos suite green; per-tenant isolation, quotas, and observability in place. ✅
   (Except the P2-002 follow-up above for the four experimental adapters.)

---

## Next: V2 — "World-Class Agentic Framework" roadmap

The P0–P2 work above made Chronos *correct, durable, and enterprise-safe*. The forward-looking
capability work — reaching parity with Google ADK, LangGraph 1.0, and DeepAgents while keeping
Chronos's self-hostable/Go-native/durable/governed lane — is tracked as a structured,
multi-session, multi-agent plan under [`plan/`](plan/):

- **[`plan/README.md`](plan/README.md)** — strategic thesis, workstreams, wave sequencing, dependency graph.
- **[`plan/CONVENTIONS.md`](plan/CONVENTIONS.md)** — item format, status lifecycle, review gates, layer/testing rules.
- **[`plan/STATUS.md`](plan/STATUS.md)** — live progress tracker; pick the next item here.
- **`plan/workstreams/`** — C DX & Eval Loop · D Memory & Knowledge (complete) ·
  E Model & Integrations · F Enterprise Governance. (Workstreams A Agent Harness and
  B Interop Protocols shipped in Wave 1 and were retired from the active roadmap.)

Consult `plan/STATUS.md` for the next open item before starting V2 work.
