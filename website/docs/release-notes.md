---
title: "Release Notes"
sidebar_label: "Release Notes"
---

Chronos ships as a Go module (`github.com/spawn08/chronos`), a cross-platform CLI
(`chronos`), and a container image. The CLI binaries (Linux, macOS, Windows —
amd64 + arm64), a signed Docker image, checksums, and an SBOM are published on
every tagged release; see [CLI Install](getting-started/cli-install.md).

This page tracks user-facing features. Auto-generated per-tag changelogs (commit
list) are on each [GitHub Release](https://github.com/spawn08/chronos/releases).

## v0.10.0 — "World-Class Agentic Framework" wave

A large capability wave that takes Chronos from *correct, durable, and
enterprise-safe* to *feature-competitive with the best agentic frameworks* — while
keeping its self-hostable, Go-native, durable, governed lane.

### Agent Harness — build agents that work on long, real tasks

The scaffolding that separates a demo from a product, out of the box.

- **Planning (todo) tool** — an agent maintains a first-class, revisable task list
  across turns; persisted per session so it survives checkpoints and resume.
  → [Planning guide](guides/planning.md)
- **Virtual filesystem** — `fs_write`/`fs_read`/`fs_ls`/`fs_delete` scratch space so
  large intermediate work is offloaded out of the context window and paged back by
  path. Per-session, tenant-scoped. → [Virtual Filesystem guide](guides/virtual-filesystem.md)
- **Context-isolated & dynamic subagents** — `spawn_subagent` delegates a sub-task
  to a subagent that runs in its own fresh context and returns only its result;
  subagents can be defined at runtime and run durably on the queue.
  → [Subagents guide](guides/subagents.md)
- **Automatic context compaction** — long conversations summarize older turns and
  stay within budget using a real BPE tokenizer, while pinned content and the active
  plan are always retained. → [Context Management guide](guides/context-management.md)
- **Deep-agent preset** — one call, `harness.NewDeepAgent(...)`, assembles planning +
  virtual filesystem + subagents + compaction + memory recall with sensible,
  override-able defaults. → [Deep Agent guide](guides/deep-agent.md)

### Interoperability — talk to any ecosystem

- **A2A (agent-to-agent) client + server** — expose a Chronos agent over the A2A
  protocol (agent card, task submit/get/cancel, SSE streaming) behind auth and
  tenant scoping, backed by the durable queue for resumable tasks; and delegate to a
  remote A2A agent as a subagent. → [A2A guide](guides/a2a.md)
- **MCP server** — expose Chronos tools to any MCP host (Claude Desktop, IDEs, ADK,
  LangGraph) over stdio + SSE, honoring per-tool permissions and approval.
  → [MCP guide](guides/mcp.md)
- **AG-UI standard event stream** — a standard agent-UI event protocol
  (`/api/agui/stream`) so any compatible frontend can render a run (streaming tokens,
  tool calls, plan updates, state, HITL) with no Chronos-specific glue.
  → [AG-UI guide](guides/agui.md)

### Memory & Knowledge — automatic, cross-session

- **Automatic semantic long-term recall** — cross-session recall on by default when a
  memory manager has a vector index; the top-k relevant memories are injected each
  turn. → [Semantic Recall guide](guides/semantic-recall.md)
- **RAG scaling** — batched embedding, chunking, query-embedding cache, and relevance
  thresholds are on by default for large corpora. → [Memory guide](guides/memory.md)

### Developer Experience — the eval-driven loop

- **Eval-driven loop (trace → dataset → eval → gate)** — capture real runs into a
  golden dataset, run it against an agent with evaluators (incl. LLM-as-judge), and
  gate regressions in CI. Trend history is tenant-scoped and queryable over time.
  New CLI: `chronos evals capture | gate | history`. → [Eval Loop guide](guides/eval-loop.md)

### Foundation (already shipped)

This wave builds on the completed production-hardening work: a durable, distributed,
multi-tenant execution plane (durable queue with leased dequeue, heartbeat/orphan
recovery, idempotency + outbox, back-pressure), an externalized control plane
(per-session SSE, distributed rate limiting, persisted approvals, OpenTelemetry
metrics/traces), multi-tenancy with object-level authorization, hardened auth
(OIDC/JWKS, hashed API keys, per-tenant quotas), a hardened sandbox, a production
Helm chart, and a supply-chain-secured CI (govulncheck, Trivy, gosec/CodeQL, SBOM,
cosign signing). See [PLAN.md](https://github.com/spawn08/chronos/blob/main/PLAN.md).
