# Workstream D — Memory & Knowledge — COMPLETE

> **Wave 2.** Agents now recall relevant long-term memory automatically, and RAG indexing/
> retrieval scales without manual tuning. Full delivery history: `plan/STATUS.md` progress log.

---

### WC-D-001 — Automatic semantic long-term recall
- [x] **Status:** DONE <!-- done: 2026-07-30 -->
- **Delivered:** `sdk/memory/manager.go` gained `WithVectorIndex` + `Recall`; wired into
  `sdk/agent` via `WithMemoryRecall` (default-on when a `MemoryManager` is set), scoped by
  `(agentID, userID, tenant)` and budget-aware (composes with context compaction).

---

### WC-D-002 — Finish & default RAG scaling
- [x] **Status:** DONE <!-- done: 2026-07-30 -->
- **Delivered:** `sdk/knowledge/vectordb.go` batches/chunks large-corpus indexing, caches query
  embeddings, and applies top-k + score-threshold retrieval by default; concurrent indexing is
  race-free (fixed a re-embed bug in the drain-queue path).
