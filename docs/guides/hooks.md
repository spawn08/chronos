---
title: "Hooks & Observability"
permalink: /guides/hooks/
sidebar:
  nav: "docs"
---

# Hooks & Observability

Hooks are middleware that intercept model calls, tool calls, and session events. They provide a composable way to add caching, retry logic, cost tracking, rate limiting, metrics, and logging without modifying agent code.

## Hook Interface

Every hook implements the `Hook` interface:

```go
type Hook interface {
    Before(ctx context.Context, evt *Event) error
    After(ctx context.Context, evt *Event) error
}
```

Hooks are composed into a `Chain` and fire in order: all `Before` handlers run before the operation, all `After` handlers run after.

## Event Types

```go
EventModelCallBefore  // Before an LLM API call
EventModelCallAfter   // After an LLM API call
EventToolCallBefore   // Before a tool execution
EventToolCallAfter    // After a tool execution
EventSessionStart     // When a session begins
EventSessionEnd       // When a session ends
```

## Built-in Hooks

### MetricsHook

Tracks latency, token usage, and error rates for all model and tool calls.

```go
metrics := hooks.NewMetricsHook()

// After running the agent...
summary := metrics.GetSummary()
fmt.Printf("Model calls: %d, Avg latency: %v\n", summary.TotalModelCalls, summary.AvgModelLatency)

// Per-call breakdown
for _, m := range metrics.GetMetrics() {
    fmt.Printf("  %s: %v (error=%v)\n", m.Name, m.Duration, m.Error)
}

// Reset for a new measurement window
metrics.Reset()
```

### CostTracker

Estimates API costs using configurable per-token pricing. Supports budget limits.

```go
tracker := hooks.NewCostTracker(map[string]hooks.ModelPrice{
    "gpt-4o":      {PromptPricePerToken: 0.0000025, CompletionPricePerToken: 0.00001},
    "gpt-4o-mini": {PromptPricePerToken: 0.00000015, CompletionPricePerToken: 0.0000006},
})
tracker.Budget = 1.00 // $1.00 max spend — blocks calls when exceeded

// After running...
report := tracker.GetGlobalCost()
fmt.Printf("Total: $%.4f (%d tokens)\n", report.TotalCost, report.PromptTokens+report.CompletionTokens)

// Per-session tracking
sessionReport := tracker.GetSessionCost("session-123")
```

Pass `nil` for the price table to use built-in defaults covering GPT-4o, Claude, Gemini, Mistral, and o-series models.

### CacheHook

Caches identical LLM requests to avoid duplicate API calls. Supports TTL and max entries.

```go
cache := hooks.NewCacheHook(5 * time.Minute)
cache.MaxEntries = 1000

// After running...
hits, misses := cache.Stats()
fmt.Printf("Cache: %d hits, %d misses\n", hits, misses)

// Manual clear
cache.Clear()
```

Streaming requests and tool-call responses are automatically excluded from caching.

### RetryHook

Retries failed model calls with exponential backoff and jitter. When the agent provides the model provider and request in event metadata, the hook performs actual retry calls.

```go
retry := hooks.NewRetryHook(3) // max 3 retry attempts
retry.BaseDelay = 500 * time.Millisecond
retry.MaxDelay = 10 * time.Second
retry.OnRetry = func(attempt int, delay time.Duration) {
    log.Printf("Retry attempt %d after %v", attempt, delay)
}

// Optional: classify which errors are retryable
retry.RetryableError = func(err error) bool {
    return strings.Contains(err.Error(), "rate limit") ||
           strings.Contains(err.Error(), "timeout")
}
```

### RateLimitHook

Token-bucket rate limiter for model API calls.

```go
limiter := hooks.NewRateLimitHook(
    60,  // 60 requests per minute
    0,   // no token-per-minute limit
)
limiter.WaitOnLimit = true // block until capacity is available (vs. return error)
```

### LoggingHook

Simple structured logging for all events.

```go
logger := &hooks.LoggingHook{}
```

## Composing Hooks

Hooks are added to an agent via the builder and execute in registration order:

```go
a, _ := agent.New("observed-agent", "Agent").
    WithModel(provider).
    AddHook(logger).      // fires first
    AddHook(metrics).     // fires second
    AddHook(tracker).     // fires third
    AddHook(limiter).     // fires fourth
    AddHook(cache).       // fires fifth
    AddHook(retry).       // fires last
    Build()
```

Order matters:
- Put `LoggingHook` first for comprehensive logging
- Put `RateLimitHook` before `CacheHook` so cached responses bypass the limiter
- Put `RetryHook` last so it catches errors from all earlier hooks

## Custom Hooks

Implement the `Hook` interface:

```go
type AuditHook struct {
    entries []AuditEntry
}

func (h *AuditHook) Before(_ context.Context, evt *hooks.Event) error {
    if evt.Type == hooks.EventModelCallBefore {
        h.entries = append(h.entries, AuditEntry{
            Time:  time.Now(),
            Model: evt.Name,
            Type:  "request",
        })
    }
    return nil
}

func (h *AuditHook) After(_ context.Context, evt *hooks.Event) error {
    if evt.Type == hooks.EventModelCallAfter {
        h.entries = append(h.entries, AuditEntry{
            Time:  time.Now(),
            Model: evt.Name,
            Type:  "response",
            Error: evt.Error != nil,
        })
    }
    return nil
}
```

See the [hooks_observability example](../../examples/hooks_observability/) for a complete runnable demonstration.
