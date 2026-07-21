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

:::note
Anthropic model IDs above are exact. OpenAI and Gemini IDs track the mid-2026
release lines — snapshot/date-suffixed variants (e.g. `gpt-5.5-2026-04-23`) and
newer releases may exist; consult the provider's model list before pinning one.
:::

## Supported Providers

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
// OpenAI embeddings
emb := model.NewOpenAIEmbeddings(apiKey)
emb := model.NewOpenAIEmbeddingsWithConfig(model.ProviderConfig{
    APIKey: apiKey,
    Model:  "text-embedding-3-small",
})

// Ollama local embeddings
emb := model.NewOllamaEmbeddings("http://localhost:11434", "nomic-embed-text")

// With in-memory caching
emb := model.NewCachedEmbeddings(model.NewOpenAIEmbeddings(apiKey))

// Usage
resp, _ := emb.Embed(ctx, &model.EmbeddingRequest{
    Input: []string{"Hello world", "Goodbye world"},
})
// resp.Embeddings: [][]float32
```

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

Supported YAML provider values: `openai`, `anthropic`, `gemini`, `mistral`, `ollama`, `azure`, `groq`, `together`, `deepseek`, `openrouter`, `fireworks`, `perplexity`, `anyscale`, `compatible`.
