# Azure OpenAI Examples

Chronos talks to Azure-hosted OpenAI models through the same `model.Provider`
interface as every other backend, so agents, graphs, tools, and teams work
unchanged. Azure differs from vanilla OpenAI in one way: you address a
**deployment** on a **resource endpoint** with an **api-version**, not a plain
model id.

## Environment variables

All Azure examples read the same four variables:

| Variable | Example | Purpose |
|----------|---------|---------|
| `AZURE_OPENAI_API_KEY` | `abc123...` | Resource API key |
| `AZURE_OPENAI_ENDPOINT` | `https://my-resource.openai.azure.com` | Resource endpoint |
| `AZURE_OPENAI_DEPLOYMENT` | `gpt-4o-mini` | Chat model deployment name |
| `AZURE_OPENAI_API_VERSION` | `2024-10-21` | API version |

```bash
export AZURE_OPENAI_API_KEY=<your-azure-api-key>
export AZURE_OPENAI_ENDPOINT=https://<your-resource>.openai.azure.com
export AZURE_OPENAI_DEPLOYMENT=<your-deployment-name>
export AZURE_OPENAI_API_VERSION=2024-10-21
```

Every example degrades gracefully: if `AZURE_OPENAI_API_KEY` is unset it prints
the required variables and exits `0` instead of making a network call. This
keeps `go run` and CI safe with no credentials.

## This example: `main.go` — chat and streaming

Basic single-agent chat, in both standard (full response) and streaming
(token-by-token) modes.

```bash
# Standard mode — waits for the full response, then prints
go run ./examples/azure/main.go

# Streaming mode — prints tokens as they arrive
go run ./examples/azure/main.go -stream
```

It builds a provider with `model.NewAzureOpenAIWithConfig` and wraps it in an
`agent.New(...).WithModel(provider).Build()` agent.

## More Azure examples

| Example | Demonstrates |
|---------|--------------|
| [`examples/azure_tools`](../azure_tools) | Multi-round **tool calling** (calculator + lookup) with the `StopReasonToolCall` loop |
| [`examples/azure_rag`](../azure_rag) | **RAG** with `model.NewAzureOpenAIEmbeddings`, a `knowledge.VectorKnowledge`, and a self-contained in-memory `storage.VectorStore` |

## YAML configs (no Go code)

Run the same setups declaratively through the CLI:

- [`examples/yaml-configs/providers/azure.yaml`](../yaml-configs/providers/azure.yaml) — single Azure agent
- [`examples/yaml-configs/azure-team.yaml`](../yaml-configs/azure-team.yaml) — a sequential researcher → writer → editor Azure team

```bash
CHRONOS_CONFIG=examples/yaml-configs/providers/azure.yaml go run ./cli/main.go run "Hello"
CHRONOS_CONFIG=examples/yaml-configs/azure-team.yaml go run ./cli/main.go team run azure-content "Write about durable AI agents"
```
