---
title: "Providers & Models"
---


# Providers & Models

Examples focused on wiring specific model backends — cloud providers, side-by-side comparison, and automatic failover. For the full environment-variable matrix, see [Choosing a provider](../examples.md#choosing-a-provider).

---

## multi_provider

Instantiates **every** configured provider side by side and runs the same agent through each, so you can compare OpenAI, Anthropic, Gemini, Azure OpenAI, Vertex AI, and Bedrock in one run. Export as many key sets as you like — each one that's present is added to the roster.

```bash
# One provider
OPENAI_API_KEY=sk-... go run ./examples/multi_provider/

# Several at once — the example runs each in turn
export OPENAI_API_KEY=sk-...
export AZURE_OPENAI_API_KEY=... AZURE_OPENAI_ENDPOINT=https://your-resource.openai.azure.com AZURE_OPENAI_DEPLOYMENT=gpt-4o
export GOOGLE_CLOUD_PROJECT=my-project GOOGLE_ACCESS_TOKEN=$(gcloud auth print-access-token)
export AWS_REGION=us-east-1 AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=...
go run ./examples/multi_provider/
```

**Demonstrates:**
- Building the same agent against many providers from one env-driven roster
- Uniform `model.Provider` interface across OpenAI, Azure OpenAI, Vertex AI, Bedrock, Gemini, Anthropic

---

## azure

Dedicated Azure OpenAI provider example with standard and streaming modes, showing deployment-name and API-version configuration explicitly.

```bash
export AZURE_OPENAI_API_KEY=...
export AZURE_OPENAI_ENDPOINT=https://your-resource.openai.azure.com
export AZURE_OPENAI_DEPLOYMENT=gpt-4o          # your deployment name, not the base model
export AZURE_OPENAI_API_VERSION=2024-12-01-preview
go run ./examples/azure/
go run ./examples/azure/ -stream
```

**Demonstrates:**
- `model.NewAzureOpenAIWithConfig(model.AzureConfig{...})` — endpoint + deployment + API version
- Standard (full response) and streaming (`-stream`) modes on the same provider

---

## azure_tools

Azure OpenAI with **multi-round tool calling** — a `calculator` and a `lookup` tool wired into `ChatRequest.Tools`, driven through the `StopReasonToolCall` loop until the model produces a final answer.

```bash
export AZURE_OPENAI_API_KEY=...
export AZURE_OPENAI_ENDPOINT=https://your-resource.openai.azure.com
export AZURE_OPENAI_DEPLOYMENT=gpt-4o
go run ./examples/azure_tools/
```

**Demonstrates:**
- Registering tools with JSON Schema parameters on `tool.Registry`
- The tool-call loop: detect `model.StopReasonToolCall`, execute, feed results back as `RoleTool` messages
- Bounded tool rounds to prevent runaway loops

---

## azure_rag

Retrieval-augmented generation on Azure: **Azure OpenAI embeddings** + `knowledge.VectorKnowledge`, with a self-contained in-memory `storage.VectorStore` (cosine similarity) that doubles as a reference implementation of the interface.

```bash
export AZURE_OPENAI_API_KEY=...
export AZURE_OPENAI_ENDPOINT=https://your-resource.openai.azure.com
export AZURE_OPENAI_DEPLOYMENT=gpt-4o                 # chat deployment
export AZURE_OPENAI_EMBED_DEPLOYMENT=text-embedding-3-small
go run ./examples/azure_rag/
```

**Demonstrates:**
- `model.NewAzureOpenAIEmbeddingsWithConfig(...)` — Azure embeddings provider
- `knowledge.NewVectorKnowledge(...)` — ingest, embed, similarity-search
- Implementing `storage.VectorStore` in-memory, and grounding a chat answer with retrieved context

See also the [`azure-team.yaml`](../yaml-examples.md) config for a declarative multi-agent Azure team.

---

## vertex

**Google Cloud Vertex AI** through its OpenAI-compatible endpoint. Auth uses a short-lived GCP access token (Bearer) rather than a static API key, so it works with `gcloud` credentials or workload identity.

```bash
export GOOGLE_CLOUD_PROJECT=my-gcp-project
export GOOGLE_CLOUD_LOCATION=us-central1
export VERTEX_MODEL=google/gemini-2.5-pro
export GOOGLE_ACCESS_TOKEN=$(gcloud auth print-access-token)
go run ./examples/vertex/
go run ./examples/vertex/ -stream
```

**Demonstrates:**
- Driving Vertex AI via `model.NewOpenAICompatibleWithConfig` against the `.../endpoints/openapi` base URL
- Bearer-token auth from `gcloud auth print-access-token` (rotate without code changes)
- Standard and streaming (`-stream`) modes

:::note
The same OpenAI-compatible pattern reaches any Vertex-hosted model exposed on the OpenAPI endpoint (Gemini, and partner models). Set `VERTEX_MODEL` to the model's Vertex ID.
:::

---

## fallback_provider

Automatic failover between model providers with configurable callbacks — so one vendor's outage isn't yours.

```bash
go run ./examples/fallback_provider/
```

**Demonstrates:**
- `model.NewFallbackProvider(primary, secondary, local)` — provider chain
- `OnFallback` callback for monitoring failures
- Primary succeeds → no fallback needed
- Primary fails → secondary used transparently
- All providers fail → graceful error reporting
- Streaming fallback
- Zero providers → validation error

:::tip
Mix clouds in the chain for real resilience — e.g. Azure OpenAI primary, Vertex AI secondary, Bedrock tertiary, local Ollama last. Each entry is just a `model.Provider`, so any constructor from [Choosing a provider](../examples.md#choosing-a-provider) works.
:::
