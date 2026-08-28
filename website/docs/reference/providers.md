---
title: "Model Providers"
---


# Model Providers

Chronos supports every major LLM provider through a single `Provider` interface. All providers implement both `Chat` (full response) and `StreamChat` (token-by-token streaming).

## Provider Interface

```go
type Provider interface {
    Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
    StreamChat(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error)
    Name() string
    Model() string
}
```

## Latest model IDs

Chronos never hardcodes a model — you pass the ID as a string, so any model a
provider ships works immediately. The table below lists commonly used current
model IDs (as of mid-2026). Provider catalogs change frequently; check each
provider's model list for the authoritative, up-to-date IDs and snapshots.

| Provider | Current models | Example ID |
|----------|----------------|------------|
| OpenAI | GPT-5.5, GPT-5.5 Pro, GPT-5, GPT-4o, o-series reasoning | `gpt-5.5` |
| Anthropic | Claude Opus 4.8, Claude Sonnet 5, Claude Haiku 4.5, Claude Opus 4.7, Claude Fable 5 | `claude-opus-4-8` |
| Google Gemini | Gemini 3.5 Flash, Gemini 3.1 Pro, Gemini 3 Flash, Gemini 3.1 Flash-Lite | `gemini-3.5-flash` |
| Mistral | Mistral Large, Mistral Medium, Mistral Small | `mistral-large-latest` |
| Groq / Together / Fireworks | Hosted open models (Llama, Qwen, DeepSeek, …) | provider-specific |
| DeepSeek | DeepSeek-V3, DeepSeek-R1 (reasoning) | `deepseek-chat` |
| Cohere (Go SDK only) | Command R+, Command R, Command | `command-r-plus` |
| AWS Bedrock (Go SDK only) | Claude, Titan, Llama, and other Bedrock-hosted models | `anthropic.claude-3-sonnet-20240229-v1:0` |

:::note
Anthropic model IDs above are exact. OpenAI and Gemini IDs track the mid-2026
release lines — snapshot/date-suffixed variants (e.g. `gpt-5.5-2026-04-23`) and
newer releases may exist; consult the provider's model list before pinning one.
:::

## Supported Providers

All the constructor and streaming snippets below use the `model` package (plus a little standard library):

```go
import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/spawn08/chronos/engine/model"
)
```

### OpenAI

```go
// Simple
p := model.NewOpenAI(apiKey)

// With configuration
p := model.NewOpenAIWithConfig(model.ProviderConfig{
    APIKey:     apiKey,
    Model:      "gpt-5.5",
    MaxRetries: 3,
    TimeoutSec: 60,
})
```

Supports the GPT-5.5 line (`gpt-5.5`, `gpt-5.5-pro`), GPT-5, GPT-4o/GPT-4o-mini, and the o-series reasoning models (o3, o4-mini). Older `gpt-4` / `gpt-3.5-turbo` IDs still work.

### Anthropic

```go
p := model.NewAnthropic(apiKey)
p := model.NewAnthropicWithConfig(model.ProviderConfig{
    APIKey: apiKey,
    Model:  "claude-opus-4-8",
})
```

Supports Claude Opus 4.8 (`claude-opus-4-8`), Claude Sonnet 5 (`claude-sonnet-5`), Claude Haiku 4.5 (`claude-haiku-4-5`), Claude Opus 4.7 (`claude-opus-4-7`), and Claude Fable 5 (`claude-fable-5`, most capable).

### Google Gemini

```go
p := model.NewGemini(apiKey)
p := model.NewGeminiWithConfig(model.ProviderConfig{
    APIKey: apiKey,
    Model:  "gemini-3.5-flash",
})
```

Supports the Gemini 3 line: Gemini 3.5 Flash (`gemini-3.5-flash`), Gemini 3.1 Pro (`gemini-3.1-pro`), Gemini 3 Flash (`gemini-3-flash`), and Gemini 3.1 Flash-Lite (`gemini-3.1-flash-lite`).

### Mistral

```go
p := model.NewMistral(apiKey)
```

### Cohere

```go
// Simple (modelID e.g. "command-r-plus", "command-r", "command")
p := model.NewCohere(apiKey, modelID)

// With configuration (default model: "command-r-plus")
p := model.NewCohereWithConfig(model.ProviderConfig{
    APIKey: apiKey,
    Model:  "command-r-plus",
})
```

`Cohere` is Go-SDK-only: it is not currently one of the values accepted by the
`provider:` field in `.chronos/agents.yaml` (see [YAML Configuration](#yaml-configuration)
below for the exact YAML enum). Construct it directly in Go if you need it.

### Ollama (Local, No API Key)

```go
p := model.NewOllama("http://localhost:11434", "llama3.2")
```

No API key required. Requires a running Ollama server.

### Azure OpenAI

```go
p := model.NewAzureOpenAI(endpoint, apiKey, deployment)
p := model.NewAzureOpenAIWithConfig(model.AzureConfig{
    ProviderConfig: model.ProviderConfig{
        APIKey:  apiKey,
        BaseURL: endpoint,
    },
    Deployment: "gpt-5.5",
    APIVersion: "2024-12-01-preview",
})
```

### Google Cloud Vertex AI

Vertex AI exposes an OpenAI-compatible endpoint. Use `NewOpenAICompatibleWithConfig` with a Bearer access token from `gcloud auth print-access-token` (or workload identity):

```go
project := os.Getenv("GOOGLE_CLOUD_PROJECT")
location := "us-central1"
baseURL := fmt.Sprintf(
    "https://%s-aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s/endpoints/openapi",
    location, project, location,
)

p := model.NewOpenAICompatibleWithConfig("vertex", model.ProviderConfig{
    APIKey:  os.Getenv("GOOGLE_ACCESS_TOKEN"),
    BaseURL: baseURL,
    Model:   "google/gemini-2.5-pro",
})
```

### AWS Bedrock

```go
p := model.NewBedrock(region, accessKey, secretKey, "anthropic.claude-3-5-sonnet-20241022-v2:0")

// With config
p := model.NewBedrockWithConfig(region, model.ProviderConfig{
    APIKey: accessKey,
    Model:  "anthropic.claude-3-5-sonnet-20241022-v2:0",
}, secretKey)
```

`Bedrock` is also Go-SDK-only — it is not one of the values accepted by the
`provider:` field in `.chronos/agents.yaml` (see [YAML Configuration](#yaml-configuration)).

### OpenAI-Compatible

Works with any API that follows the OpenAI chat completions format:

```go
p := model.NewOpenAICompatible("my-server", "http://localhost:8080/v1", "", "my-model")
```

Pass an empty API key for servers that don't require authentication.

### Convenience Constructors

Pre-configured for popular hosted providers:

```go
model.NewTogether(apiKey, modelID)    // Together AI
model.NewGroq(apiKey, modelID)        // Groq
model.NewDeepSeek(apiKey, modelID)    // DeepSeek
model.NewOpenRouter(apiKey, modelID)  // OpenRouter
model.NewFireworks(apiKey, modelID)   // Fireworks AI
model.NewPerplexity(apiKey, modelID)  // Perplexity
model.NewAnyscale(apiKey, modelID)    // Anyscale Endpoints
```

## FallbackProvider

Wraps multiple providers and tries each in order. If the primary fails, it automatically falls back to the next:

```go
fallback, _ := model.NewFallbackProvider(
    model.NewOpenAI(primaryKey),
    model.NewAnthropic(backupKey),
    model.NewOllama("http://localhost:11434", "llama3.2"),
)

fallback.OnFallback = func(index int, name string, err error) {
    log.Printf("Provider %d (%s) failed: %v", index, name, err)
}
```

## Embedding Providers

For RAG pipelines and knowledge base search:

```go
// OpenAI embeddings (default model: text-embedding-3-small)
emb := model.NewOpenAIEmbeddings(apiKey)
emb := model.NewOpenAIEmbeddingsWithConfig(model.ProviderConfig{
    APIKey: apiKey,
    Model:  "text-embedding-3-small",
})

// Ollama local embeddings (default model: nomic-embed-text)
emb := model.NewOllamaEmbeddings("http://localhost:11434", "nomic-embed-text")

// Azure OpenAI embeddings (endpoint + deployment, e.g. "text-embedding-3-large")
emb := model.NewAzureOpenAIEmbeddings(endpoint, apiKey, deployment)
emb := model.NewAzureOpenAIEmbeddingsWithConfig(model.ProviderConfig{
    APIKey:  apiKey,
    BaseURL: endpoint,
}, deployment, "2024-02-01")

// Google AI (Gemini) embeddings (default model: text-embedding-004)
emb := model.NewGoogleEmbeddings(apiKey, "text-embedding-004")

// Cohere embeddings (default model: embed-english-v3.0)
emb := model.NewCohereEmbeddings(apiKey, "embed-english-v3.0")

// With in-memory caching (wraps any EmbeddingsProvider)
emb := model.NewCachedEmbeddings(model.NewOpenAIEmbeddings(apiKey))

// Usage
resp, _ := emb.Embed(ctx, &model.EmbeddingRequest{
    Input: []string{"Hello world", "Goodbye world"},
})
// resp.Embeddings: [][]float32
```

Embeddings providers are Go-SDK-only (there is no YAML config surface for them):
you pass the API key as a constructor argument, typically read from an env var
of your choosing in application code.

| Constructor | Import | Suggested env var | Default model |
|-------------|--------|--------------------|----------------|
| `model.NewOpenAIEmbeddings(apiKey)` | `engine/model` | `OPENAI_API_KEY` | `text-embedding-3-small` |
| `model.NewOllamaEmbeddings(baseURL, modelID)` | `engine/model` | none (local server) | `nomic-embed-text` |
| `model.NewAzureOpenAIEmbeddings(endpoint, apiKey, deployment)` | `engine/model` | `AZURE_OPENAI_API_KEY` | none — `deployment` is required |
| `model.NewGoogleEmbeddings(apiKey, modelID)` | `engine/model` | `GOOGLE_API_KEY` / `GEMINI_API_KEY` | `text-embedding-004` |
| `model.NewCohereEmbeddings(apiKey, modelID)` | `engine/model` | `COHERE_API_KEY` | `embed-english-v3.0` |
| `model.NewCachedEmbeddings(inner)` | `engine/model` | — (wraps another provider) | n/a |

## Streaming

Every provider supports streaming via `StreamChat`:

```go
ch, _ := provider.StreamChat(ctx, &model.ChatRequest{
    Messages: []model.Message{
        {Role: model.RoleSystem, Content: "You are a helpful assistant."},
        {Role: model.RoleUser, Content: "Tell me a story"},
    },
})

for chunk := range ch {
    fmt.Print(chunk.Content) // tokens arrive incrementally
}
fmt.Println()
```

## YAML Configuration

Providers can be configured in YAML for CLI use:

```yaml
agents:
  - id: my-agent
    model:
      provider: openai          # or anthropic, gemini, mistral, ollama, azure, groq, etc.
      model: gpt-5.5
      api_key: ${OPENAI_API_KEY}
```

Supported YAML provider values: `openai`, `anthropic`, `gemini` (or `google`), `mistral`, `ollama`, `azure`, `groq`, `together`, `deepseek`, `openrouter`, `fireworks`, `perplexity`, `anyscale`, `compatible` (or `custom`).

:::note Go-SDK-only providers
`Cohere` and `AWS Bedrock` are **not** in this enum — they can only be constructed
directly in Go (`model.NewCohere`, `model.NewBedrock`), not selected via
`provider:` in `.chronos/agents.yaml`. Embeddings providers (OpenAI, Ollama,
Azure, Google, Cohere) are Go-SDK-only entirely; there is no YAML config surface
for embeddings.
:::
