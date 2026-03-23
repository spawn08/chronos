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

## Supported Providers

### OpenAI

```go
// Simple
p := model.NewOpenAI(apiKey)

// With configuration
p := model.NewOpenAIWithConfig(model.ProviderConfig{
    APIKey:     apiKey,
    Model:      "gpt-4o",
    MaxRetries: 3,
    TimeoutSec: 60,
})
```

Supports: GPT-4o, GPT-4o-mini, GPT-4, GPT-3.5-turbo, o1, o1-mini, o3, o3-mini.

### Anthropic

```go
p := model.NewAnthropic(apiKey)
p := model.NewAnthropicWithConfig(model.ProviderConfig{
    APIKey: apiKey,
    Model:  "claude-sonnet-4-6",
})
```

Supports: Claude Sonnet 4, Claude 3.5 Sonnet, Claude 3 Opus, Claude 3 Haiku.

### Google Gemini

```go
p := model.NewGemini(apiKey)
p := model.NewGeminiWithConfig(model.ProviderConfig{
    APIKey: apiKey,
    Model:  "gemini-2.0-flash",
})
```

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
    Deployment: "gpt-4o",
    APIVersion: "2024-12-01-preview",
})
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
      model: gpt-4o
      api_key: ${OPENAI_API_KEY}
```

Supported YAML provider values: `openai`, `anthropic`, `gemini`, `mistral`, `ollama`, `azure`, `groq`, `together`, `deepseek`, `openrouter`, `fireworks`, `perplexity`, `anyscale`, `compatible`.
