---
title: "Middleware & Hooks"
permalink: /guides/middleware/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
---

Chronos uses a hook-based middleware system to intercept execution events. Hooks run before and after model calls, tool calls, graph nodes, and session lifecycle events. Use them for logging, metrics, retries, rate limiting, cost tracking, and caching.

## Hook Interface

Every hook implements the `Hook` interface:

```go
type Hook interface {
    Before(ctx context.Context, evt *Event) error
    After(ctx context.Context, evt *Event) error
}
```

- **Before**: Called before the event executes. Return an error to abort the operation.
- **After**: Called after the event executes. Runs even when the operation fails.

## Chain

Hooks are composed in a `Chain` (a slice of `Hook`). The chain runs `Before` in order and `After` in reverse (middleware unwinding pattern).

```go
chain := hooks.Chain{hook1, hook2, hook3}
if err := chain.Before(ctx, evt); err != nil {
    return err  // hook1 or hook2 or hook3 aborted
}
// ... execute operation ...
chain.After(ctx, evt)  // hook3, hook2, hook1
```

## Event Struct

Each hook receives an `Event`:

```go
type Event struct {
    Type     EventType      // kind of event
    Name     string         // tool name, model name, or node ID
    Input    any            // request or input state
    Output   any            // response or output state
    Error    error          // error if operation failed
    Metadata map[string]any // extensible metadata
}
```

## Event Types

| Type | When Fired |
|------|------------|
| `tool_call.before` | Before a tool is executed |
| `tool_call.after` | After a tool returns |
| `model_call.before` | Before an LLM chat request |
| `model_call.after` | After an LLM response |
| `node.before` | Before a graph node runs |
| `node.after` | After a graph node completes |
| `context.overflow` | When context limit is exceeded (before summarization) |
| `context.summarize` | After summarization completes |
| `session.start` | When a session chat begins |
| `session.end` | When a session chat ends |

## Adding Hooks

Attach hooks via the agent builder:

```go
a, err := agent.New("my-agent", "My Agent").
    WithModel(provider).
    AddHook(loggingHook).
    AddHook(metricsHook).
    Build()
```

## Built-in Hooks

### LoggingHook

Records events to a slice for inspection or logging. Useful for debugging and audit trails.

```go
loggingHook := &hooks.LoggingHook{}
a.AddHook(loggingHook)

// After execution
for _, evt := range loggingHook.Events {
    fmt.Printf("%s: %s\n", evt.Type, evt.Name)
}
```

### RetryHook

Retries failed model calls with exponential backoff and jitter. The hook signals retry via event metadata; the caller (agent) must implement the actual retry loop.

```go
retry := hooks.NewRetryHook(3)
retry.BaseDelay = 500 * time.Millisecond
retry.MaxDelay = 30 * time.Second
retry.RetryableError = func(err error) bool {
    return strings.Contains(err.Error(), "rate limit")
}
retry.OnRetry = func(attempt int, delay time.Duration) {
    log.Printf("Retry attempt %d after %v", attempt, delay)
}

a.AddHook(retry)
```

| Field | Type | Description |
|-------|------|-------------|
| `MaxRetries` | int | Maximum retry attempts (default 3) |
| `BaseDelay` | time.Duration | Initial delay (default 500ms) |
| `MaxDelay` | time.Duration | Cap on delay (default 30s) |
| `RetryableError` | func(error) bool | Classify errors; nil = retry all |
| `OnRetry` | func(int, time.Duration) | Callback before each retry |
| `Retries` | int | Total retries performed (observability) |

When a retry is signaled, the hook sets `evt.Metadata["retry"] = true`, `retry_attempt`, and `retry_delay`.

### RateLimitHook

Enforces per-provider rate limits using a token-bucket algorithm. Blocks on `model_call.before` when the limit would be exceeded (or returns an error if `WaitOnLimit` is false).

```go
rl := hooks.NewRateLimitHook(60, 100000)  // 60 req/min, 100K tokens/min
rl.WaitOnLimit = true  // block until capacity available (default)

a.AddHook(rl)
```

| Field | Type | Description |
|-------|------|-------------|
| `RequestsPerMinute` | int | Max model calls per minute; 0 = unlimited |
| `TokensPerMinute` | int | Max prompt tokens per minute; 0 = unlimited |
| `WaitOnLimit` | bool | Block (true) or return error (false); default true |

### CostTracker

Tracks LLM API costs per session and globally. Enforces an optional budget by blocking model calls when the limit is exceeded.

```go
tracker := hooks.NewCostTracker(nil)  // nil = use built-in price table
tracker.Budget = 10.0  // $10 max spend; 0 = unlimited

a.AddHook(tracker)

// After execution
global := tracker.GetGlobalCost()
sessionCost := tracker.GetSessionCost(sessionID)
fmt.Printf("Total: $%.4f, Session: $%.4f\n", global.TotalCost, sessionCost.TotalCost)
```

| Field | Type | Description |
|-------|------|-------------|
| `Budget` | float64 | Max total spend (USD); 0 = unlimited |

Built-in price table includes GPT-4o, GPT-4o-mini, GPT-4-turbo, o1, o1-mini, o3, o3-mini, Claude models, Gemini, and Mistral. Pass a custom `map[string]ModelPrice` to `NewCostTracker` to override.

### CacheHook

Caches LLM responses for identical requests. Uses SHA-256 of the serialized request as the cache key. Skips streaming and tool-call responses.

```go
cache := hooks.NewCacheHook(5 * time.Minute)
cache.MaxEntries = 1000  // LRU eviction when exceeded; 0 = unlimited

a.AddHook(cache)

hits, misses := cache.Stats()
fmt.Printf("Cache: %d hits, %d misses\n", hits, misses)
```

| Field | Type | Description |
|-------|------|-------------|
| `TTL` | time.Duration | How long entries remain valid (default 5 min) |
| `MaxEntries` | int | Max cache size; 0 = unlimited |
| `Hits` | int | Cache hits |
| `Misses` | int | Cache misses |

### MetricsHook

Tracks latency, token usage, and error rates for model and tool calls.

```go
metrics := hooks.NewMetricsHook()
a.AddHook(metrics)

// Raw metrics
calls := metrics.GetMetrics()
for _, c := range calls {
    fmt.Printf("%s %s: %v\n", c.Type, c.Name, c.Duration)
}

// Aggregated summary
summary := metrics.GetSummary()
fmt.Printf("Model calls: %d, avg latency: %v, errors: %d\n",
    summary.TotalModelCalls, summary.AvgModelLatency, summary.TotalErrors)
```

| MetricsSummary Field | Description |
|---------------------|-------------|
| `TotalModelCalls` | Number of model calls |
| `TotalToolCalls` | Number of tool calls |
| `TotalErrors` | Calls that failed |
| `TotalPromptTokens` | Sum of prompt tokens |
| `TotalCompTokens` | Sum of completion tokens |
| `AvgModelLatency` | Average model call duration |
| `AvgToolLatency` | Average tool call duration |
| `MaxModelLatency` | Longest model call |
| `MaxToolLatency` | Longest tool call |

## Complete Example: Combining Hooks

```go
package main

import (
    "context"
    "log"
    "os"
    "time"

    "github.com/spawn08/chronos/engine/hooks"
    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
)

func main() {
    ctx := context.Background()

    provider := model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

    logging := &hooks.LoggingHook{}
    metrics := hooks.NewMetricsHook()
    cost := hooks.NewCostTracker(nil)
    cost.Budget = 5.0
    cache := hooks.NewCacheHook(10 * time.Minute)
    rl := hooks.NewRateLimitHook(30, 50000)

    a, err := agent.New("monitored-agent", "Monitored Agent").
        WithModel(provider).
        WithSystemPrompt("You are a helpful assistant.").
        AddHook(logging).
        AddHook(metrics).
        AddHook(cost).
        AddHook(cache).
        AddHook(rl).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    resp, err := a.Chat(ctx, "What is 2 + 2?")
    if err != nil {
        log.Fatal(err)
    }
    log.Println(resp.Content)

    summary := metrics.GetSummary()
    log.Printf("Model calls: %d, latency: %v, cost: $%.4f",
        summary.TotalModelCalls, summary.AvgModelLatency, cost.GetGlobalCost().TotalCost)

    hits, misses := cache.Stats()
    log.Printf("Cache: %d hits, %d misses", hits, misses)
}
```
