# Workstream D — Memory & Knowledge

> **Wave 2.** LangGraph's long-term store and DeepAgents' persistent memory auto-inject relevant
> memories without the developer wiring recall by hand. Chronos has the machinery
> (`sdk/memory/memory.go` `Store`, `sdk/memory/manager.go` LLM-powered `Manager`, tenant-scoped
> per P0-008/P2-002) and a RAG stack (`sdk/knowledge/vectordb.go`, `cache.go`, `loaders/`) —
> but recall is manual and the RAG scaling work (PLAN.md P2-009) needs finishing and defaulting.

---

### WC-D-001 — Automatic semantic long-term recall
- [ ] **Status:** REVIEW
- **Problem:** Memories are stored but not automatically retrieved and injected. Developers must
  call the `Manager` explicitly; agents don't "remember" across sessions by default.
- **Location:** `sdk/memory/manager.go`, `sdk/memory/memory.go`, wired into
  `sdk/agent/agent.go` `buildChatRequest` (agent.go:253) behind `WithMemoryManager` (agent.go:110);
  embeddings via `model.EmbeddingsProvider`; storage via `storage.Storage` + `VectorStore`,
  tenant-scoped through `storage/tenant.go`.
- **Action:** On each turn, semantically retrieve the top-k relevant long-term memories for the
  `(agentID, userID, tenant)` scope and inject them into context (respecting the compaction
  budget from WC-A-004). Auto-write salient memories after a turn via the existing `Manager`.
  Make recall a policy toggle (default on when a `MemoryManager` is set).
- **Acceptance:** An agent recalls a fact stated in a prior session in a new session with no
  explicit developer recall call; two users/tenants never cross-recall (extends
  `sdk/memory/isolation_test.go`).
- **Depends on:** none (unblocks WC-A-004 and WC-A-005).
- **Tests:** cross-session recall test with a mock embeddings provider; tenant/user isolation
  test; a relevance test asserting top-k ordering.

---

### WC-D-002 — Finish & default RAG scaling
- [ ] **Status:** TODO
- **Problem:** PLAN.md P2-009 scoped batching/chunking/query-embedding cache/relevance
  thresholds for `sdk/knowledge/vectordb.go`; ensure it is complete, on by default, and safe for
  large corpora and concurrent indexing.
- **Location:** `sdk/knowledge/vectordb.go`, `sdk/knowledge/cache.go`, `sdk/knowledge/options.go`,
  `sdk/knowledge/loaders/`.
- **Action:** Confirm/finish: batched embedding of large corpora (no single-call blowups),
  chunking in the indexing path, a query-embedding cache, and top-k + score-threshold retrieval.
  Guard concurrent indexing. Make these the defaults via `options.go`. (Re-verify against the
  `scaling_test.go` already present.)
- **Acceptance:** Indexing a large corpus does not fail on a single embed call; retrieval returns
  only above-threshold top-k; concurrent indexing is race-free; defaults are sane without tuning.
- **Depends on:** none.
- **Tests:** large-corpus indexing test with a mock embeddings provider; threshold/top-k tests;
  `-race` concurrent-indexing test (extend `sdk/knowledge/scaling_test.go`).
