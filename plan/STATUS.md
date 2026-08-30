# V2 Roadmap — Live Status

> Single source of truth for progress. **Update this in the same PR that changes an item's
> state.** Pick the lowest-wave `TODO` whose dependencies are all `DONE`. See
> [`CONVENTIONS.md`](CONVENTIONS.md) for the status lifecycle.

Legend: `TODO` · `IN-PROGRESS` · `REVIEW` · `DONE`

## Wave 1 — Parity foundations — COMPLETE

> Workstream A (Agent Harness: planning tool, virtual FS, subagents, compaction, deep-agent
> preset) shipped in full and has been retired from the active roadmap — see the progress log
> below for delivery detail and `plan/wc-a-*` branches for history.

| Item | Title | Status | Owner / Agent | Branch | Depends on |
|------|-------|:------:|---------------|--------|------------|
| WC-B-001 | A2A client + server | DONE | claude | plan/wc-b-a2a | none |
| WC-B-002 | MCP server | DONE | claude | plan/wc-b-mcp-server | none |
| WC-B-003 | AG-UI standard event stream | DONE | claude | plan/wc-b-agui | none |

## Wave 2 — Make it lovable

| Item | Title | Status | Owner / Agent | Branch | Depends on |
|------|-------|:------:|---------------|--------|------------|
| WC-C-001 | Eval-driven loop (trace→dataset→eval→gate) | DONE | claude | plan/wc-c-eval-loop | none |
| WC-C-002 | Visual studio / graph debugger | DONE | claude | plan/wc-c-dashboard | B-003 |
| WC-C-004 | YAML agents as durable, dashboard-visible ChronosOS runs | DONE | claude | plan/wc-c-yaml-graph | C-002 |
| WC-C-003 | One-command deploy | TODO | — | — | C-002 |
| WC-D-001 | Automatic semantic long-term recall | DONE | claude | plan/wc-d-recall | none |
| WC-D-002 | Finish & default RAG scaling | DONE | claude | plan/wc-d-rag-scaling | none |

## Wave 3 — Breadth & enterprise lead

| Item | Title | Status | Owner / Agent | Branch | Depends on |
|------|-------|:------:|---------------|--------|------------|
| WC-E-001 | Live bidirectional (audio/video) streaming | TODO | — | — | B-003 |
| WC-E-002 | Curated connectors + plugin SDK | TODO | — | — | B-002 |
| WC-F-001 | Per-tenant budget & quota policy engine | TODO | — | — | none |
| WC-F-002 | Model allow-lists & data-residency/PII policy | TODO | — | — | F-001 |
| WC-F-003 | Compliance-grade audit & export | TODO | — | — | F-001, F-002 |

## Progress log

Append a line when an item changes state (newest first):

```
YYYY-MM-DD  WC-X-000  TODO→IN-PROGRESS  agent/owner  plan/wc-x-slug  note
```

2026-08-29  WC-C-004  TODO→DONE  claude  plan/wc-c-yaml-graph  YAML agents as durable, dashboard-visible ChronosOS runs: declarative graph schema on AgentConfig (sdk/agent/graphbuild.go — model/tool/subagent/passthrough node types, static + conditional edges, compiles to a real graph.StateGraph via the existing Agent.WithGraph) + Durable opt-in flag; chronos serve (buildServeGraphOptions) now optionally loads agents YAML and registers every durable agent's compiled graph into chronosos.WithGraphs automatically; new POST /api/dashboard/runs closes a deeper gap found while building this — no HTTP endpoint anywhere in ChronosOS could start a brand-new run, only resume/time-travel a session some in-process caller had already created (affected Go-defined graphs too, not just YAML); also added `chronos auth token` (mints a dev API key/JWT matching CHRONOS_AUTH, closing a real onboarding gap) and fixed two confirmed doc inaccuracies in server.md; example examples/yaml_dashboard runs via chronos serve alone with zero Go code; both review gates ran fresh and converged on 2 CRITICALs each (handleStartRun's caller-supplied session_id was a cross-tenant existence oracle/squatting vector; buildServeGraphOptions's per-agent storage was silently irrelevant to where sessions actually persist; the "tool" node bypassed tool.Registry.Execute's permission/approval enforcement; the "model" node bypassed Agent.Chat's guardrails/hooks/memory) plus 5 BLOCKs (per-agent storage leak, config-parse errors swallowed like no-config, dead WithPeerAgents API, examples/yaml_dashboard duplicating the registry loop with no nil-safety, input_key type mismatches silently falling back) — all fixed in-branch; one NOTE (no subagent reference-cycle detection) logged as a known follow-up rather than fixed; verified end-to-end against a running server (start→pause at gate→resume→completed) after the fixes, in addition to sdk/agent, os/dashboard, and cli/cmd unit tests; full repo -race suite green
2026-08-29  WC-C-002  TODO→DONE  claude  plan/wc-c-dashboard  visual studio / graph debugger: os/dashboard (checkpoints/graph-topology/cost/resume/time-travel API, reusing existing sessions/traces/agui-stream/approval endpoints rather than duplicating them) + embedded no-CDN static UI at /dashboard/ (Swagger-style auth bypass for the shell only) + engine/graph.ToJSON + chronosos WithGraphs/WithCostTracker/WithDashboard options; fixed 2 bugs found building on top of checkpoints: Runner never synced RunState.Status onto storage.Session.Status (dashboard/CLI couldn't see "paused"), and the in-memory adapter aliased a mutable State map across checkpoints + ordered GetLatestCheckpoint by wall-clock (P0-003's bug, missed in this adapter); both review gates ran fresh and converged on the same CRITICAL (cross-tenant cost IDOR) plus 3 BLOCKs (404-vs-501 conflation, degraded 413/400 body-size handling, unescaped innerHTML in app.js) — all fixed in-branch, including a new optional storage.SessionStatusUpdater (narrow status-only write) to close a Session-record race the status-sync fix introduced; example + docs; full repo -race suite green
2026-07-31  WC-C-001  TODO→DONE  claude  plan/wc-c-eval-loop  eval-driven loop: CaptureFromSession + DatasetRunner(Target seam) + Gate(regression) + tenant-scoped checkpoint-backed ReportStore + CLI capture/gate/history + CI eval-gate job; both gates run, fixed CRITICAL tenant collision (reworked onto checkpoints) + BLOCK strict flag parsing
2026-07-31  WC-B-001  TODO→DONE  claude  plan/wc-b-a2a  A2A client+server recovered onto main (PR #40 had merged to the wrong branch); TaskStore seam + tenant-partitioned memStore + queue-backed DurableStore + SSE + NewRemoteAgentTool + WithA2A(auth+tenant); both gates run fresh, 2 blockers fixed (A2A body cap; DurableStore.Get error masking) + padding tests replaced
2026-07-31  WC-A-005  TODO→DONE  claude  plan/wc-a-deep-agent  deep-agent preset: NewDeepAgent assembles planning+VFS+subagents+compaction(plan pinned)+recall with sensible defaults, all override-able; example + docs; both review gates APPROVED. Completes Workstream A.
2026-07-30  WC-A-004  TODO→DONE  claude  plan/wc-a-compaction  automatic context compaction: real BPE tokenizer budget + static/dynamic pins (always retained) + enforceContextBudget bound + example + docs; both review gates APPROVED
2026-07-30  WC-D-001  TODO→DONE  claude  plan/wc-d-recall  automatic semantic long-term recall (Manager.WithVectorIndex + Recall + embed-on-write) + agent WithMemoryRecall (default-on, WC-A-004 seam) + example + docs
2026-07-30  WC-D-002  TODO→DONE  claude  plan/wc-d-rag-scaling  RAG scaling verified on-by-default; drain-queue fix in Load (no re-embed) + large-corpus/drain/-race tests
2026-07-30  WC-B-003  TODO→DONE  claude  plan/wc-b-agui  AG-UI standard event stream (translator + /api/agui/stream) + example + docs
2026-07-29  WC-B-002  TODO→DONE  claude  plan/wc-b-mcp-server  MCP server (stdio + SSE) exposing tool.Registry, permission/approval-honoring
2026-07-29  WC-A-003  TODO→DONE  claude  plan/wc-a-subagents  context-isolated + dynamic subagents (spawn tool, in-process + durable queued runner) + example + docs
2026-07-29  WC-A-002  TODO→DONE  claude  plan/wc-a-vfs  virtual filesystem (SessionFileStore + VFS + fs_* tools) + example + docs
2026-07-29  WC-A-001  TODO→DONE  claude  plan/wc-a-planning-tool  planning tool + durable PlanStore + example + docs

<!-- 2026-07-29  plan created; all items TODO -->
