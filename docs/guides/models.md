---
title: "Model Providers"
permalink: /guides/models/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
---

Chronos uses a pluggable provider interface for LLM backends. All providers implement the same interface, so you can swap OpenAI for Anthropic, Ollama, or any OpenAI-compatible endpoint without changing your agent code.

## Provider Interface

```go
type Provider interface {
    Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
    StreamChat(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error)
    Name() string
    Model() string
}
```

| Method | Description |
|--------|--------------|
| `Chat` | Sends a request and returns a complete response |
| `StreamChat` | Returns a channel of partial responses; channel closes when complete |
| `Name` | Human-readable provider identifier |
| `Model` | Default model ID for this provider |

## Provider Table

| Provider | Constructor | Notes |
|----------|--------------|-------|
| OpenAI | `model.NewOpenAI(apiKey)` | GPT-4, GPT-4o, GPT-3.5-turbo, o1, o3 |
| Anthropic | `model.NewAnthropic(apiKey)` | Claude models |
| Gemini | `model.NewGemini(apiKey)` | Google Gemini |
| Mistral | `model.NewMistral(apiKey)` | Mistral models |
| Ollama | `model.NewOllama(host, model)` | Local models (e.g., `http://localhost:11434`, `llama3.2`) |
| Azure | `model.NewAzureOpenAI(endpoint, key, deployment)` | Azure OpenAI |
| Compatible | `model.NewOpenAICompatible(name, url, key, model)` | Any OpenAI-compatible API |

## Convenience Constructors

For providers that expose an OpenAI-compatible API, Chronos provides convenience constructors:

| Constructor | Base URL | Use Case |
|-------------|----------|----------|
| `model.NewTogether(apiKey, modelID)` | api.together.xyz | Together AI |
| `model.NewGroq(apiKey, modelID)` | api.groq.com | Groq |
| `model.NewDeepSeek(apiKey, modelID)` | api.deepseek.com | DeepSeek |
| `model.NewOpenRouter(apiKey, modelID)` | openrouter.ai | OpenRouter (multi-model) |
| `model.NewFireworks(apiKey, modelID)` | api.fireworks.ai | Fireworks AI |
| `model.NewPerplexity(apiKey, modelID)` | api.perplexity.ai | Perplexity |
| `model.NewAnyscale(apiKey, modelID)` | api.endpoints.anyscale.com | Anyscale Endpoints |

Example:

```go
provider := model.NewGroq(os.Getenv("GROQ_API_KEY"), "llama-3.1-70b-versatile")
```

## ProviderConfig

For full configuration, use `ProviderConfig` with the `WithConfig` constructor:

```go
cfg := model.ProviderConfig{
    APIKey:        os.Getenv("OPENAI_API_KEY"),
    BaseURL:       "https://api.openai.com/v1",
    Model:         "gpt-4o",
    MaxRetries:    3,
    TimeoutSec:    60,
    OrgID:         "org-xxx",
    ContextWindow: 128000,
}

provider := model.NewOpenAIWithConfig(cfg)
```

| Field | Type | Description |
|-------|------|--------------|
| `APIKey` | string | Authentication key |
| `BaseURL` | string | API base URL (optional for most providers) |
| `Model` | string | Model identifier |
| `MaxRetries` | int | Retry count on transient failures |
| `TimeoutSec` | int | Request timeout in seconds |
| `OrgID` | string | Organization ID (OpenAI) |
| `ContextWindow` | int | Override default context window size |

## ChatRequest

Input to a chat completion:

| Field | Type | Description |
|-------|------|--------------|
| `Model` | string | Override provider default |
| `Messages` | []Message | Conversation messages |
| `MaxTokens` | int | Maximum tokens to generate |
| `Temperature` | float64 | Sampling temperature (0-2) |
| `TopP` | float64 | Nucleus sampling |
| `Stream` | bool | Enable streaming |
| `Tools` | []ToolDefinition | Function definitions for tool calling |
| `Stop` | []string | Stop sequences |
| `ResponseFormat` | string | `"json_object"` for JSON mode |

## ChatResponse

Output of a chat completion:

| Field | Type | Description |
|-------|------|--------------|
| `ID` | string | Response ID |
| `Content` | string | Generated text |
| `Role` | string | Usually `"assistant"` |
| `Usage` | Usage | Token counts |
| `ToolCalls` | []ToolCall | Requested tool invocations |
| `StopReason` | StopReason | Why generation stopped |
| `Delta` | bool | True when streaming partial response |

## Usage

```go
type Usage struct {
    PromptTokens     int
    CompletionTokens int
}
```

## StopReason Constants

| Constant | Value | Meaning |
|----------|-------|---------|
| `StopReasonEnd` | `"end"` | Natural completion |
| `StopReasonMaxTokens` | `"max_tokens"` | Hit token limit |
| `StopReasonToolCall` | `"tool_call"` | Model requested tool execution |
| `StopReasonFilter` | `"content_filter"` | Content filter triggered |

## Streaming

Use `StreamChat` for token-by-token streaming. The returned channel receives partial `ChatResponse` values with `Delta: true`; the final response may include usage and `StopReason`.

```go
ch, err := provider.StreamChat(ctx, &model.ChatRequest{
    Messages: messages,
    Stream:   true,
})
if err != nil {
    log.Fatal(err)
}

for resp := range ch {
    if resp.Content != "" {
        fmt.Print(resp.Content)
    }
}
fmt.Println()
```

## Embeddings Providers

For RAG and vector search, use an `EmbeddingsProvider`:

```go
type EmbeddingsProvider interface {
    Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)
}
```

| Constructor | Description |
|-------------|--------------|
| `model.NewOpenAIEmbeddings(apiKey)` | OpenAI text-embedding-3-small |
| `model.NewOpenAIEmbeddingsWithConfig(cfg)` | With full config |
| `model.NewOllamaEmbeddings(baseURL, modelID)` | Local embeddings via Ollama |
| `model.NewCachedEmbeddings(inner)` | In-memory cache wrapper |

Example:

```go
embedder := model.NewOpenAIEmbeddings(os.Getenv("OPENAI_API_KEY"))
cached := model.NewCachedEmbeddings(embedder)
```

## FallbackProvider

`FallbackProvider` tries multiple providers in order. If the primary fails, it automatically falls back to the next. Useful for primary-cloud to cheaper-model or cloud to local-Ollama failover.

```go
primary := model.NewOpenAI(openAIKey)
fallback := model.NewOllama("http://localhost:11434", "llama3.2")

provider, err := model.NewFallbackProvider(primary, fallback)
if err != nil {
    log.Fatal(err)
}

// Optional: log when fallback occurs
provider.OnFallback = func(index int, name string, err error) {
    log.Printf("Provider %s failed, trying next: %v", name, err)
}

resp, err := provider.Chat(ctx, req)
```

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/chronos-ai/chronos/engine/model"
    "github.com/chronos-ai/chronos/sdk/agent"
)

func main() {
    ctx := context.Background()

    provider := model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

    a, err := agent.New("demo", "Demo Agent").
        WithModel(provider).
        WithSystemPrompt("You are a concise assistant.").
        Build()
    if err != nil {
        log.Fatal(err)
    }

    resp, err := a.Chat(ctx, "Say hello in one sentence.")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp.Content)
    fmt.Printf("Tokens: %d prompt, %d completion\n",
        resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
}
```
