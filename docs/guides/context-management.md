---
title: "Context Management"
permalink: /guides/context-management/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
---

LLMs have finite context windows. Long conversations eventually exceed the limit, causing truncation or API errors. Chronos provides automatic context window management with rolling summarization so multi-turn sessions stay within budget without losing important context.

## The Problem

Each model has a maximum context size (e.g., 128K tokens for GPT-4o). As a conversation grows:

- System prompts, instructions, memories, and knowledge consume tokens
- Each user and assistant message adds more
- Tool calls and results add further overhead
- Eventually the combined size exceeds the limit

Without management, the API rejects the request or truncates history, losing earlier context.

## The Solution

Chronos automatically detects when a session approaches its context limit and triggers rolling summarization. Older messages are compressed into a concise summary; recent turns are preserved intact so the model retains immediate conversational context. The prior summary is incorporated into each new summary so nothing is lost across multiple summarization passes.

## TokenCounter Interface

Token counting is abstracted behind the `TokenCounter` interface in `engine/model`:

```go
type TokenCounter interface {
    CountTokens(messages []Message) int
    CountString(s string) int
}
```

- `CountTokens` returns the estimated token count for a slice of messages, including per-message overhead (role, separators) and tool calls.
- `CountString` returns the estimated token count for a raw string.

## EstimatingCounter

The default implementation uses a character-ratio heuristic: approximately 4 characters per token. This avoids external dependencies (e.g., tiktoken) and works across models.

```go
counter := model.NewEstimatingCounter()

// Estimate tokens for a string
tokens := counter.CountString("Hello, world!")
// tokens ≈ 4 (12 chars / 4)

// Estimate tokens for messages
msgs := []model.Message{
    {Role: model.RoleUser, Content: "What is the capital of France?"},
}
total := counter.CountTokens(msgs)
```

You can customize the ratio:

```go
counter := &model.EstimatingCounter{CharsPerToken: 3.5}
```

## Model Context Limits Registry

Chronos maintains built-in context limits for well-known models. Use `ContextLimit` to resolve the limit for a given model:

```go
limit := model.ContextLimit("gpt-4o", 0)      // 128000
limit := model.ContextLimit("claude-3-5-sonnet", 0)  // 200000
limit := model.ContextLimit("gemini-1.5-pro", 0)      // 2097152
limit := model.ContextLimit("unknown-model", 8192)   // fallback 8192
```

Built-in limits include:

| Provider | Models | Limit (tokens) |
|----------|--------|----------------|
| OpenAI | gpt-4o, gpt-4o-mini, gpt-4-turbo | 128K |
| OpenAI | o1, o3, o3-mini, o4-mini | 200K |
| Anthropic | claude-sonnet-4-6, claude-3-5-sonnet, claude-3-opus, claude-3-haiku | 200K |
| Google | gemini-2.0-flash, gemini-2.0-pro, gemini-1.5-flash | 1M |
| Google | gemini-1.5-pro | 2M |
| Mistral | mistral-large-latest | 128K |
| Meta | llama3.3, llama3.2, llama3.1 | 131K |
| DeepSeek | deepseek-chat, deepseek-coder, deepseek-reasoner | 64K |

Unknown models use the provided fallback, or 8192 if fallback is 0.

## ContextConfig

`ContextConfig` controls context window behavior on the agent:

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `MaxContextTokens` | int | Override model default; 0 = use model default | 0 |
| `SummarizeThreshold` | float64 | Fraction of context window that triggers summarization | 0.8 |
| `PreserveRecentTurns` | int | Number of recent user/assistant pairs to keep | 5 |

```go
agent.WithContextConfig(agent.ContextConfig{
    MaxContextTokens:    128000,
    SummarizeThreshold:  0.8,
    PreserveRecentTurns: 5,
})
```

## Summarizer

The `Summarizer` compresses older messages into a rolling summary using an LLM. It is constructed with a provider, token counter, and configuration:

```go
summarizer := model.NewSummarizer(provider, counter, model.SummarizationConfig{
    Threshold:           0.8,
    PreserveRecentTurns: 5,
    MaxSummaryTokens:    1024,
    Prompt:             "",  // optional custom prompt
})
```

### SummarizationConfig

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `Threshold` | float64 | Fraction of context window that triggers summarization | 0.8 |
| `PreserveRecentTurns` | int | Recent user/assistant pairs to keep unsummarized | 5 |
| `MaxSummaryTokens` | int | Cap on generated summary length | 1024 |
| `Prompt` | string | Custom summarization system prompt | built-in |

### NeedsSummarization

Check whether summarization is needed before calling `Summarize`:

```go
if summarizer.NeedsSummarization(systemTokens, messages, contextLimit) {
    result, err := summarizer.Summarize(ctx, priorSummary, messages)
    // ...
}
```

### Summarize

`Summarize` compresses older messages, preserves recent turns, and incorporates any prior summary:

```go
result, err := summarizer.Summarize(ctx, priorSummary, messages)
if err != nil {
    return err
}
// result.Summary: new rolling summary
// result.PreservedMessages: recent messages kept intact
```

## Rolling Summarization Flow

1. **Detect overflow**: When `systemTokens + messageTokens > threshold * contextLimit`, summarization is triggered.
2. **Split messages**: Old messages (to summarize) are separated from recent turns (to preserve).
3. **Build input**: Prior summary (if any) plus old messages are formatted for the summarizer.
4. **Call LLM**: The provider produces a concise summary.
5. **Replace context**: `priorSummary` becomes the new summary; `messages` becomes `PreservedMessages`.
6. **Persist**: Summary and preserved messages are stored in the event ledger.

## ChatWithSession Integration

`ChatWithSession` automatically triggers summarization when approaching the context limit. No extra code is required:

1. Load session events from storage
2. Append the new user message
3. Build system context (prompt, instructions, memories, knowledge)
4. Resolve context limit (from `ContextConfig.MaxContextTokens` or model registry)
5. If `NeedsSummarization` is true: fire `EventContextOverflow`, run summarizer, persist, fire `EventSummarization`
6. Build final message array: system context + prior summary (if any) + preserved messages
7. Call the model

## YAML Configuration

Context settings can be specified in agent YAML:

```yaml
agents:
  - id: my-agent
    name: My Agent
    context:
      max_tokens: 0
      summarize_threshold: 0.8
      preserve_recent_turns: 5
```

| Field | Description |
|-------|-------------|
| `context.max_tokens` | Override model default; 0 = use model default |
| `context.summarize_threshold` | Fraction that triggers summarization |
| `context.preserve_recent_turns` | Recent turns to keep |

## Hook Events

When summarization occurs, hooks receive:

- **EventContextOverflow** (before summarization): `Metadata` includes `estimated_tokens` and `context_limit`
- **EventSummarization** (after summarization): `Metadata` includes `summary_length` and `preserved_messages`

```go
a.AddHook(&MyHook{})

// In Before/After:
if evt.Type == hooks.EventContextOverflow {
    estimated := evt.Metadata["estimated_tokens"].(int)
    limit := evt.Metadata["context_limit"].(int)
    // log or alert
}
if evt.Type == hooks.EventSummarization {
    length := evt.Metadata["summary_length"].(int)
    preserved := evt.Metadata["preserved_messages"].(int)
}
```

## ProviderConfig.ContextWindow

Model providers can override the default context window via `ProviderConfig.ContextWindow`. This is used when constructing providers (e.g., for custom deployments). The agent's `ContextConfig.MaxContextTokens` takes precedence over the model registry; provider-level `ContextWindow` is an alternative way to set the limit when building the provider.

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
    "github.com/chronos-ai/chronos/storage/adapters/sqlite"
)

func main() {
    ctx := context.Background()

    store, err := sqlite.New("session.db")
    if err != nil {
        log.Fatal(err)
    }
    defer store.Close()
    if err := store.Migrate(ctx); err != nil {
        log.Fatal(err)
    }

    a, err := agent.New("session-agent", "Session Agent").
        WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
        WithStorage(store).
        WithSystemPrompt("You are a helpful assistant.").
        WithContextConfig(agent.ContextConfig{
            MaxContextTokens:    0,   // use model default
            SummarizeThreshold:  0.8,
            PreserveRecentTurns: 5,
        }).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    sessionID := "user-123-conv-1"

    resp1, err := a.ChatWithSession(ctx, sessionID, "My name is Alice.")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp1.Content)

    resp2, err := a.ChatWithSession(ctx, sessionID, "What is my name?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp2.Content)

    // After many turns, when context approaches the limit,
    // summarization runs automatically. No code changes needed.
}
```
