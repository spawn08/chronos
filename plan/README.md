# Chronos V2 — "World-Class Agentic Framework" Roadmap

> Forward-looking capability roadmap that builds **on top of** the completed
> production-hardening work in [`../PLAN.md`](../PLAN.md). Where `PLAN.md` made Chronos
> *correct, durable, and enterprise-safe*, this roadmap makes it *competitive with the
> best agentic frameworks in the market* (Google ADK, LangGraph 1.0, DeepAgents) while
> keeping Chronos's unique lane: **self-hostable, Go-native, durable, enterprise-governed.**
>
> This is a **multi-session, multi-agent** plan. Read [`CONVENTIONS.md`](CONVENTIONS.md)
> before starting any item, and update [`STATUS.md`](STATUS.md) as you work.

## Strategic thesis

Chronos already owns the hardest-to-build layer: a genuinely durable, distributed,
multi-tenant, **self-hostable** runtime (durable queue, leased dequeue, idempotency+outbox,
back-pressure, externalized control plane). Competitors need a *managed platform* to match it.

Two of the four gaps identified for this roadmap have already shipped: the agent harness
(planning, virtual FS, context-isolated & dynamic subagents, auto-compaction) and
interoperability (A2A, MCP server, AG-UI stream) both completed in Wave 1 — see
`plan/STATUS.md`'s progress log for delivery detail. What remains:

1. **DX + eval-driven loop** — the trace→dataset→eval→gate loop shipped; a visual studio/debugger
   and one-command deploy do not. This is what made LangGraph "the default." → Workstream **C**.
2. **Enterprise governance** (lead, don't follow) — per-tenant budgets, policy, and compliance
   export as a first-class control-plane product no competitor offers self-hosted. → Workstream **F**.

Chronos should **not** try to out-ecosystem LangChain. It should win on
*durable + self-hosted + governed*, and finish the remaining DX and governance work.

## Workstreams

| ID | Workstream | File | Theme |
|----|-----------|------|-------|
| **C** | DX & Eval Loop | [`workstreams/C-dx-eval-loop.md`](workstreams/C-dx-eval-loop.md) | Eval-driven dev loop (done), visual studio/debugger, one-command deploy |
| **D** | Memory & Knowledge | [`workstreams/D-memory-knowledge.md`](workstreams/D-memory-knowledge.md) | Automatic semantic recall, RAG finalize — **complete** |
| **E** | Model & Integrations | [`workstreams/E-model-integrations.md`](workstreams/E-model-integrations.md) | Live bidirectional streaming, connector suite + plugin SDK |
| **F** | Enterprise Governance | [`workstreams/F-enterprise-governance.md`](workstreams/F-enterprise-governance.md) | Budgets/quotas policy engine, model allow-lists, compliance export |

Workstreams **A** (Agent Harness) and **B** (Interop Protocols) shipped in full during Wave 1
and have been retired from the active roadmap; their history lives in `plan/STATUS.md`'s
progress log and the `plan/wc-a-*` / `plan/wc-b-*` branches.

## Wave sequencing

Waves run in order. Within a wave, workstreams are independent and **parallelizable across agents**.

```
Wave 1 — Parity foundations               COMPLETE (Agent Harness + Interop Protocols shipped)
Wave 2 — Make it lovable
    C  DX & Eval Loop (eval loop done)    D  Memory & Knowledge (complete)
        │
Wave 3 — Breadth & enterprise lead
    E  Model & Integrations     F  Enterprise Governance
```

## Dependency graph (critical path)

```
C-002 studio    ◄── engine/graph/visualize.go (exists), B-003 AG-UI stream (shipped)
C-003 one-command deploy ◄── C-002
E-001 live stream ◄── engine/stream (exists)
F-001 budgets   ◄── engine/hooks/{cost,ratelimit} + os/auth + storage/tenant (exist)
```

## Where to start

1. Open [`STATUS.md`](STATUS.md), pick the first `TODO` item in the lowest open wave whose
   dependencies are all `DONE`.
2. Follow the per-item spec in the workstream file (Problem → Location → Action → Acceptance → Tests).
3. Follow [`CONVENTIONS.md`](CONVENTIONS.md) for branch, tests, review gates, and commit.
4. Flip the item's checkbox and update `STATUS.md` in the same PR.
