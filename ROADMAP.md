# Chronos Roadmap — Delivered Capabilities

> Historical record of the completed build-out roadmap. All 101 tracked items
> (P0–P3) were delivered. Forward-looking, production-hardening work now lives in
> **[PLAN.md](PLAN.md)**.

## Summary

| Priority | Items | Status |
|----------|-------|--------|
| P0 — Critical bugs & wiring fixes | 16 | ✅ Complete |
| P1 — Core feature gaps (parity)   | 28 | ✅ Complete |
| P2 — Ecosystem & developer experience | 30 | ✅ Complete |
| P3 — Ecosystem expansion          | 27 | ✅ Complete |
| **Total** | **101** | **✅ Complete** |

## What was delivered

**P0 — bug fixes & wiring:** Redis list methods; RedisVector search parsing; RetryHook
actual retries; `NumHistoryRuns` wired into context; `OutputSchema` JSON-schema pass +
validation; Runner → SSE Broker events; trace collector wired into agent/graph execution;
CLI `sessions resume` and `config set`/`model`; testing foundation for core packages.

**P1 — core features:** MCP (stdio) support; subgraphs & graph composition; time travel
(checkpoint fork/replay); advanced streaming; human-in-the-loop enhancements; context
management & summarization; API-server auth/security building blocks; evaluation framework;
in-memory storage adapter; health & lifecycle endpoints.

**P2 — ecosystem & DX:** built-in toolkits; knowledge-base document loaders; multimodal
messages (image/file/audio); functional graph API; graph visualization; observability
(metrics/OTel scaffolding); scheduler; additional guardrails; agent features.

**P3 — expansion:** additional model providers (incl. Bedrock, Cohere) and embeddings
providers; additional vector stores (pgvector, pinecone, weaviate, milvus, chromadb,
lancedb); interface integrations; advanced multi-agent patterns (swarm, hierarchy);
reasoning strategies; sandbox backends (container/wasm/k8s scaffolding); CLI enhancements;
migration framework.

## Note on maturity

Several delivered items are functional but **prototype-grade** or have known correctness
and scale gaps identified in the production-readiness review. Those are captured, prioritized,
and scheduled in **[PLAN.md](PLAN.md)** — treat PLAN.md as the source of truth for what to
build and fix next.
