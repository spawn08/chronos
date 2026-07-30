# V2 Roadmap — Live Status

> Single source of truth for progress. **Update this in the same PR that changes an item's
> state.** Pick the lowest-wave `TODO` whose dependencies are all `DONE`. See
> [`CONVENTIONS.md`](CONVENTIONS.md) for the status lifecycle.

Legend: `TODO` · `IN-PROGRESS` · `REVIEW` · `DONE`

## Wave 1 — Parity foundations

| Item | Title | Status | Owner / Agent | Branch | Depends on |
|------|-------|:------:|---------------|--------|------------|
| WC-A-001 | Planning ("todo") tool | DONE | claude | plan/wc-a-planning-tool | none |
| WC-A-002 | Virtual filesystem (context offloading) | DONE | claude | plan/wc-a-vfs | none |
| WC-A-003 | Context-isolated & dynamic subagents | DONE | claude | plan/wc-a-subagents | A-001, A-002 |
| WC-A-004 | Automatic context compaction | DONE | claude | plan/wc-a-compaction | A-001, D-001 |
| WC-A-005 | "Deep agent" harness preset | TODO | — | — | A-001…004, D-001 |
| WC-B-001 | A2A client + server | TODO | — | — | A-003 |
| WC-B-002 | MCP server | DONE | claude | plan/wc-b-mcp-server | none |
| WC-B-003 | AG-UI standard event stream | DONE | claude | plan/wc-b-agui | A-001 |

## Wave 2 — Make it lovable

| Item | Title | Status | Owner / Agent | Branch | Depends on |
|------|-------|:------:|---------------|--------|------------|
| WC-C-001 | Eval-driven loop (trace→dataset→eval→gate) | TODO | — | — | none |
| WC-C-002 | Visual studio / graph debugger | TODO | — | — | B-003 |
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

2026-07-30  WC-A-004  TODO→DONE  claude  plan/wc-a-compaction  automatic context compaction: real BPE tokenizer budget + static/dynamic pins (always retained) + enforceContextBudget bound + example + docs; both review gates APPROVED
2026-07-30  WC-D-001  TODO→DONE  claude  plan/wc-d-recall  automatic semantic long-term recall (Manager.WithVectorIndex + Recall + embed-on-write) + agent WithMemoryRecall (default-on, WC-A-004 seam) + example + docs
2026-07-30  WC-D-002  TODO→DONE  claude  plan/wc-d-rag-scaling  RAG scaling verified on-by-default; drain-queue fix in Load (no re-embed) + large-corpus/drain/-race tests
2026-07-30  WC-B-003  TODO→DONE  claude  plan/wc-b-agui  AG-UI standard event stream (translator + /api/agui/stream) + example + docs
2026-07-29  WC-B-002  TODO→DONE  claude  plan/wc-b-mcp-server  MCP server (stdio + SSE) exposing tool.Registry, permission/approval-honoring
2026-07-29  WC-A-003  TODO→DONE  claude  plan/wc-a-subagents  context-isolated + dynamic subagents (spawn tool, in-process + durable queued runner) + example + docs
2026-07-29  WC-A-002  TODO→DONE  claude  plan/wc-a-vfs  virtual filesystem (SessionFileStore + VFS + fs_* tools) + example + docs
2026-07-29  WC-A-001  TODO→DONE  claude  plan/wc-a-planning-tool  planning tool + durable PlanStore + example + docs

<!-- 2026-07-29  plan created; all items TODO -->
