# V2 Roadmap — Live Status

> Single source of truth for progress. **Update this in the same PR that changes an item's
> state.** Pick the lowest-wave `TODO` whose dependencies are all `DONE`. See
> [`CONVENTIONS.md`](CONVENTIONS.md) for the status lifecycle.

Legend: `TODO` · `IN-PROGRESS` · `REVIEW` · `DONE`

## Wave 1 — Parity foundations

| Item | Title | Status | Owner / Agent | Branch | Depends on |
|------|-------|:------:|---------------|--------|------------|
| WC-A-001 | Planning ("todo") tool | DONE | claude | plan/wc-a-planning-tool | none |
| WC-A-002 | Virtual filesystem (context offloading) | TODO | — | — | none |
| WC-A-003 | Context-isolated & dynamic subagents | TODO | — | — | A-001, A-002 |
| WC-A-004 | Automatic context compaction | TODO | — | — | A-001, D-001 |
| WC-A-005 | "Deep agent" harness preset | TODO | — | — | A-001…004, D-001 |
| WC-B-001 | A2A client + server | TODO | — | — | A-003 |
| WC-B-002 | MCP server | TODO | — | — | none |
| WC-B-003 | AG-UI standard event stream | TODO | — | — | A-001 |

## Wave 2 — Make it lovable

| Item | Title | Status | Owner / Agent | Branch | Depends on |
|------|-------|:------:|---------------|--------|------------|
| WC-C-001 | Eval-driven loop (trace→dataset→eval→gate) | TODO | — | — | none |
| WC-C-002 | Visual studio / graph debugger | TODO | — | — | B-003 |
| WC-C-003 | One-command deploy | TODO | — | — | C-002 |
| WC-D-001 | Automatic semantic long-term recall | TODO | — | — | none |
| WC-D-002 | Finish & default RAG scaling | TODO | — | — | none |

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

2026-07-29  WC-A-001  TODO→DONE  claude  plan/wc-a-planning-tool  planning tool + durable PlanStore + example + docs

<!-- 2026-07-29  plan created; all items TODO -->
