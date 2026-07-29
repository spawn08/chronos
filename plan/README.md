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

The gaps are three, plus one place Chronos can **lead**:

1. **The agent harness** — the scaffolding (planning, context offloading, context-isolated
   subagents, compaction) that makes agents work on long, real tasks. DeepAgents proved this
   is what separates a demo from a product. → Workstream **A**.
2. **Interoperability** — A2A (agent-to-agent), MCP *server*, and a standard UI event stream.
   ADK is betting the ecosystem on A2A; it is becoming table stakes. → Workstream **B**.
3. **DX + eval-driven loop** — a visual studio and a trace→dataset→eval→gate loop. This is what
   made LangGraph "the default." → Workstream **C**.
4. **Enterprise governance** (lead, don't follow) — per-tenant budgets, policy, and compliance
   export as a first-class control-plane product no competitor offers self-hosted. → Workstream **F**.

Chronos should **not** try to out-ecosystem LangChain. It should win on
*durable + self-hosted + governed*, and reach feature parity on harness, interop, and DX.

## Workstreams

| ID | Workstream | File | Theme |
|----|-----------|------|-------|
| **A** | Agent Harness | [`workstreams/A-agent-harness.md`](workstreams/A-agent-harness.md) | Planning, virtual FS, context-isolated & dynamic subagents, auto-compaction |
| **B** | Interop Protocols | [`workstreams/B-interop-protocols.md`](workstreams/B-interop-protocols.md) | A2A client+server, MCP server, AG-UI event schema |
| **C** | DX & Eval Loop | [`workstreams/C-dx-eval-loop.md`](workstreams/C-dx-eval-loop.md) | Eval-driven dev loop, visual studio/debugger, one-command deploy |
| **D** | Memory & Knowledge | [`workstreams/D-memory-knowledge.md`](workstreams/D-memory-knowledge.md) | Automatic semantic recall, cross-session long-term store, RAG finalize |
| **E** | Model & Integrations | [`workstreams/E-model-integrations.md`](workstreams/E-model-integrations.md) | Live bidirectional streaming, connector suite + plugin SDK |
| **F** | Enterprise Governance | [`workstreams/F-enterprise-governance.md`](workstreams/F-enterprise-governance.md) | Budgets/quotas policy engine, model allow-lists, compliance export |

## Wave sequencing

Waves run in order. Within a wave, workstreams are independent and **parallelizable across agents**.

```
Wave 1 — Parity foundations (highest leverage)
    A  Agent Harness            B  Interop Protocols
        │                            │
Wave 2 — Make it lovable
    C  DX & Eval Loop           D  Memory & Knowledge
        │                            │
Wave 3 — Breadth & enterprise lead
    E  Model & Integrations     F  Enterprise Governance
```

## Dependency graph (critical path)

```
A-001 planning tool ─► A-002 virtual FS ─► A-003 subagents ─► A-004 compaction ─► A-005 harness preset
                                                    │
B-002 MCP server ◄── engine/tool.Registry (exists)  │
B-001 A2A ─────────────────────────────────────────┘ (subagents ↔ remote agents)
                                                    │
C-001 eval loop ◄── evals/ + os/trace (exist)       │
C-002 studio    ◄── engine/graph/visualize.go (exists)
D-001 auto-recall ◄── sdk/memory/manager.go (exists) ─► used by A-005 harness preset
E-001 live stream ◄── engine/stream (exists)
F-001 budgets   ◄── engine/hooks/{cost,ratelimit} + os/auth + storage/tenant (exist)
```

## Where to start

1. Open [`STATUS.md`](STATUS.md), pick the first `TODO` item in the lowest open wave whose
   dependencies are all `DONE`.
2. Follow the per-item spec in the workstream file (Problem → Location → Action → Acceptance → Tests).
3. Follow [`CONVENTIONS.md`](CONVENTIONS.md) for branch, tests, review gates, and commit.
4. Flip the item's checkbox and update `STATUS.md` in the same PR.
