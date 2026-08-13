---
title: "Model Providers"
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
| OpenAI | `model.NewOpenAI(apiKey)` | GPT-5.5, GPT-5, GPT-4o, o-series (o3, o4-mini) |
| Anthropic | `model.NewAnthropic(apiKey)` | Claude Opus 4.8, Sonnet 5, Haiku 4.5, Fable 5 |
| Gemini | `model.NewGemini(apiKey)` | Gemini 3.5 Flash, 3.1 Pro, 3 Flash |
| Mistral | `model.NewMistral(apiKey)` | Mistral Large / Medium / Small |
| Ollama | `model.NewOllama(host, model)` | Local models (e.g., `http://localhost:11434`, `llama3.2`) |
| Azure | `model.NewAzureOpenAI(endpoint, key, deployment)` | Azure OpenAI |
| Bedrock | `model.NewBedrock(region, accessKey, secretKey, modelID)` | AWS Bedrock (Claude, Llama, Titan, …) |
| Vertex AI | `model.NewOpenAICompatibleWithConfig("vertex", cfg)` | Google Cloud Vertex AI via OpenAI-compatible endpoint |
| Compatible | `model.NewOpenAICompatible(name, url, key, model)` | Any OpenAI-compatible API |

### Google Cloud Vertex AI

Vertex AI exposes an OpenAI-compatible endpoint, so Chronos drives it through `NewOpenAICompatibleWithConfig`. Auth uses a short-lived GCP access token as the Bearer credential:

```go
project := os.Getenv("GOOGLE_CLOUD_PROJECT")
location := "us-central1"
baseURL := fmt.Sprintf(
    "https://%s-aiplatform.googleapis.com/v1beta1/projects/%s/locations/%s/endpoints/openapi",
    location, project, location,
)

provider := model.NewOpenAICompatibleWithConfig("vertex", model.ProviderConfig{
    APIKey:  os.Getenv("GOOGLE_ACCESS_TOKEN"), // gcloud auth print-access-token
    BaseURL: baseURL,
    Model:   "google/gemini-2.5-pro",
})
```

### AWS Bedrock

```go
provider := model.NewBedrock(
    os.Getenv("AWS_REGION"),
    os.Getenv("AWS_ACCESS_KEY_ID"),
    os.Getenv("AWS_SECRET_ACCESS_KEY"),
    "anthropic.claude-3-5-sonnet-20241022-v2:0",
)
```

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
    Model:         "gpt-5.5",
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
| `Reasoning` | *ReasoningConfig | Provider-native reasoning/thinking settings |

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
| `Reasoning` | string | Provider-approved reasoning output, kept separate from final answer text |
| `Delta` | bool | True when streaming partial response |

## Native reasoning and thinking

Use `ReasoningConfig` to request native reasoning without embedding provider-specific request fields in application code:

```go
req := &model.ChatRequest{
    Messages: messages,
    Reasoning: &model.ReasoningConfig{
        Enabled:      true,
        Effort:       "high",
        BudgetTokens: 4096,
        Summary:      true,
    },
}
```

| Field | Description |
|-------|-------------|
| `Enabled` | Request native reasoning when the provider supports it. When false, all other native reasoning fields are ignored. |
| `Effort` | Normalized effort (`low`, `medium`, `high`); mapped to OpenAI-compatible `reasoning_effort` |
| `BudgetTokens` | Thinking budget; mapped to Anthropic `thinking` and Gemini `thinkingConfig` |
| `Summary` | Request provider-approved reasoning/thought output where supported |

YAML agents expose the same controls plus Chronos prompt strategies:

```yaml
reasoning:
  strategy: reflection  # none, cot, reflection
  native: true
  effort: high
  budget_tokens: 4096
  summary: true
```

Provider and model support varies. `Content` always remains the final answer; available reasoning is returned separately in `ChatResponse.Reasoning`. Applications should avoid displaying reasoning unless the user explicitly requested it.

:::caution Native reasoning with tools
Anthropic and Gemini attach signed thought blocks to tool calls. Chronos currently fails closed when native reasoning and tools are combined for those providers, rather than dropping signatures and producing an invalid follow-up request. OpenAI-compatible reasoning with tools remains available. Use prompt strategy reasoning or disable tools until signed thought-block preservation is supported.
:::

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

    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
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
