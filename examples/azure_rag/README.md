# Azure OpenAI — RAG (Retrieval-Augmented Generation)

End-to-end RAG on Azure OpenAI: embed documents with an Azure embeddings
deployment, retrieve the most relevant ones for a question, and feed them as
grounding context into an Azure chat completion.

## What it demonstrates

- `model.NewAzureOpenAIEmbeddingsWithConfig` as a `model.EmbeddingsProvider`
- `knowledge.NewVectorKnowledge` — `AddDocuments` + `Load` to index, `Search`
  to retrieve top-k
- A **self-contained in-memory `storage.VectorStore`** (`MemoryVectorStore`)
  using cosine similarity. Chronos has no built-in in-memory vector store, so
  this file doubles as a reference implementation of the `storage.VectorStore`
  interface (`CreateCollection`, `Upsert`, `Search`, `Delete`, `Close`)
- Grounded answering: retrieved documents are injected as context with an
  instruction to answer only from that context

## Run

This example needs **two** deployments — one chat and one embeddings:

```bash
export AZURE_OPENAI_API_KEY=<your-azure-api-key>
export AZURE_OPENAI_ENDPOINT=https://<your-resource>.openai.azure.com
export AZURE_OPENAI_DEPLOYMENT=<your-chat-deployment>             # e.g. gpt-4o-mini
export AZURE_OPENAI_EMBED_DEPLOYMENT=<your-embeddings-deployment> # e.g. text-embedding-3-small
export AZURE_OPENAI_API_VERSION=2024-10-21

go run ./examples/azure_rag/main.go
```

`embedDimension` in `main.go` is set to `1536` (text-embedding-3-small); change
it to match your embeddings deployment (e.g. `3072` for
text-embedding-3-large).

Without `AZURE_OPENAI_API_KEY` the example prints the required variables and
exits `0` — no network call.

## Tests

`main_test.go` is fully offline: it tests `MemoryVectorStore` (upsert/replace,
delete, top-k) and `cosineSimilarity` ranking with tiny deterministic vectors.
It never contacts Azure.

```bash
go test ./examples/azure_rag/...
```

See also: [`examples/azure`](../azure) (chat/streaming) and
[`examples/azure_tools`](../azure_tools) (tool calling).
