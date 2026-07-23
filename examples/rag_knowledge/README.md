# RAG / Knowledge

Retrieval-Augmented Generation with `knowledge.VectorKnowledge`.

This example is **fully offline** by default. It ships:

- `store.go` — a self-contained in-memory `storage.VectorStore` (cosine similarity).
- `embedder.go` — a deterministic, hashing `model.EmbeddingsProvider` (no API key, no network).

`main.go` ingests four short documents, embeds and indexes them, runs a
similarity search, and prints the top-k retrieved passages. If an LLM provider
is configured in the environment it also feeds the retrieved passages to the
model to produce a grounded answer.

## Run

```bash
# Retrieval only — offline, no keys
go run ./examples/rag_knowledge/

# Also generate a grounded answer with a real LLM
OPENAI_API_KEY=sk-... go run ./examples/rag_knowledge/
```

## Going to production

Swap the two stubs for real implementations:

- `storage.VectorStore` → `storage/adapters/qdrant`
- `model.EmbeddingsProvider` → a real embeddings provider

The `knowledge.VectorKnowledge` code stays identical.

## Test

```bash
go test ./examples/rag_knowledge/   # asserts the most relevant doc ranks first
```
