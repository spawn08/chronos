---
title: "Automatic Semantic Long-Term Recall"
sidebar_label: "Semantic Recall"
---


Chronos agents can *store* long-term memories, but by default a developer had to
retrieve them explicitly, and retrieval dumped **every** memory into the prompt.
**Semantic recall** closes that loop: on each turn the agent embeds the user's
message, retrieves the top-k most relevant long-term memories for that
`(agent, user)` scope, and injects only those — so an agent "remembers" across
sessions without any explicit recall call, and without flooding the context.

It builds on [Memory](/guides/memory): attach a vector index to a
`memory.Manager` and recall turns on automatically.

## Quick start

```go
import (
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/sdk/memory"
)

// A Manager with a semantic index: an EmbeddingsProvider + a VectorStore.
mgr := memory.NewManager("assistant", userID, memStore, provider).
    WithVectorIndex(embedder, vectorStore, "text-embedding-3-small", 1536)

a, _ := agent.New("assistant", "Assistant").
    WithModel(model).
    WithUserID(userID).
    WithMemoryManager(mgr). // recall is ON by default once the index is attached
    Build()
```

That's it. Facts the agent stores (via LLM extraction or the `remember` tool)
are embedded on write; on the next turn the relevant ones are recalled and
injected as a `Relevant user memories:` system message.

## How it works

1. **Embed-on-write.** `ExtractMemories` and the `remember` tool mirror each
   stored fact into the vector index, keyed by the same tenant-scoped ID as the
   relational record (so re-storing a key overwrites its vector). `forget` and
   `OptimizeMemories` keep the index in sync.
2. **Recall-on-turn.** `Manager.Recall(ctx, query, topK)` embeds the query,
   searches the per-agent collection, and returns candidates ranked by score.
3. **Injection.** The agent formats the top-k into one system message. Recall
   returns *structured, ranked candidates* — never a pre-formatted blob — so a
   future context-compaction budget can trim them before they reach the prompt.

## Tenant isolation

Memories are scoped by `(agentID, userID)`. `Recall` passes the tenant scope
token to the vector store as a metadata filter (`storage.WithFilter`), so the
store computes top-k **within** the caller's subset of a shared per-agent
collection — one user never recalls another's memories, even when they share a
collection and store identical text. Adapters that store metadata structurally
(Qdrant, pgvector, Pinecone, Chroma) apply the filter server-side; the others
filter client-side. `Recall` also re-checks the scope on every result as a
defense-in-depth guarantee, so a misbehaving adapter can never leak across
tenants.

## Configuration

Recall is controlled with `WithMemoryRecall`:

```go
a, _ := agent.New("assistant", "Assistant").
    WithMemoryManager(mgr).
    WithMemoryRecall(agent.RecallConfig{
        TopK:           8,    // memories to recall per turn (default 5)
        ScoreThreshold: 0.75, // drop weak matches (default 0: keep all)
        Disabled:       false, // set true to fall back to the full-memory dump
    }).
    Build()
```

When recall is disabled, or the manager has no vector index, the agent falls
back to the legacy behavior (inject all stored memories), so existing agents are
unaffected.

## Try it

```bash
go run ./examples/semantic_recall/
```

The example is fully offline (a hashing embedder + an in-memory vector store):
it writes memories in one "session", recalls them by relevance in another, and
shows that a second user recalls none of the first user's memories.
