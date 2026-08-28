---
title: "Cost Tracking"
---


The `CostTracker` hook monitors LLM API costs in real time. It accumulates token usage per session and globally, computes costs using a configurable price table, and optionally enforces budget limits.

## Quick Setup

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/spawn08/chronos/engine/hooks"
    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
)

func main() {
    ctx := context.Background()
    key := os.Getenv("OPENAI_API_KEY")

    tracker := hooks.NewCostTracker(nil) // uses default price table

    a, _ := agent.New("assistant", "Assistant").
        WithModel(model.NewOpenAI(key)).
        AddHook(tracker).
        Build()

    resp, _ := a.Chat(ctx, "Summarize this document...")
    fmt.Println(resp.Content)

    report := tracker.GetGlobalCost()
    fmt.Printf("Tokens: %d prompt + %d completion\n",
        report.PromptTokens, report.CompletionTokens)
    fmt.Printf("Cost: $%.6f\n", report.TotalCost)
}
```

## CostReport

```go
type CostReport struct {
    PromptTokens     int     `json:"prompt_tokens"`
    CompletionTokens int     `json:"completion_tokens"`
    TotalTokens      int     `json:"total_tokens"`
    TotalCost        float64 `json:"total_cost"`
    Currency         string  `json:"currency"`
}
```

## Custom Price Table

Override the default prices by passing a custom price table:

```go
prices := map[string]hooks.ModelPrice{
    "gpt-5.5": {
        PromptPricePerToken:     0.000005,
        CompletionPricePerToken: 0.00003,
    },
    "claude-opus-4-8": {
        PromptPricePerToken:     0.000005,
        CompletionPricePerToken: 0.000025,
    },
}

tracker := hooks.NewCostTracker(prices)
```

### Default Price Table

The built-in table covers these models (prices as of mid-2026 — override with a custom table for current rates):

| Model | Prompt (per token) | Completion (per token) |
|-------|-------------------|----------------------|
| gpt-5.5 | $0.000005 | $0.00003 |
| gpt-5 | $0.00000125 | $0.00001 |
| gpt-4o | $0.0000025 | $0.00001 |
| gpt-4o-mini | $0.00000015 | $0.0000006 |
| gpt-4-turbo | $0.00001 | $0.00003 |
| o3 | $0.00001 | $0.00004 |
| o3-mini | $0.0000011 | $0.0000044 |
| claude-fable-5 | $0.00001 | $0.00005 |
| claude-opus-4-8 | $0.000005 | $0.000025 |
| claude-opus-4-7 | $0.000005 | $0.000025 |
| claude-sonnet-5 | $0.000003 | $0.000015 |
| claude-sonnet-4-6 | $0.000003 | $0.000015 |
| claude-haiku-4-5 | $0.000001 | $0.000005 |
| claude-3-5-sonnet | $0.000003 | $0.000015 |
| claude-3-opus | $0.000015 | $0.000075 |
| claude-3-haiku | $0.00000025 | $0.00000125 |
| claude-3-5-haiku | $0.0000008 | $0.000004 |
| gemini-2.0-flash | $0.00000015 | $0.0000006 |
| gemini-1.5-pro | $0.00000125 | $0.000005 |
| mistral-large-latest | $0.000002 | $0.000006 |

See [`engine/hooks/cost.go`](https://github.com/spawn08/chronos/blob/main/engine/hooks/cost.go) for the authoritative, up-to-date table.

## Budget Enforcement

Set a maximum spend. When the budget is exceeded, the hook returns an error on the next model call, preventing further API charges:

```go
tracker := hooks.NewCostTracker(nil)
tracker.Budget = 5.00 // $5.00 maximum

a, _ := agent.New("budget-agent", "Budget Agent").
    WithModel(model.NewOpenAI(key)).
    AddHook(tracker).
    Build()

// After spending $5.00, subsequent calls return:
// "cost budget exceeded: spent $5.0012 of $5.0000 budget"
```

## Per-Session Tracking

Track costs for individual sessions:

```go
// After running several sessions
sessionReport := tracker.GetSessionCost("sess_abc123")
globalReport := tracker.GetGlobalCost()

fmt.Printf("Session cost: $%.4f\n", sessionReport.TotalCost)
fmt.Printf("Global cost:  $%.4f\n", globalReport.TotalCost)
```

To associate model calls with sessions, set `session_id` in event metadata. The `ChatWithSession` method does this automatically.

## Combining with Other Hooks

Cost tracking works alongside other middleware. Order matters -- place the cost tracker after retry hooks so retried calls are counted:

```go
a, _ := agent.New("production", "Production Agent").
    WithModel(model.NewOpenAI(key)).
    AddHook(hooks.NewRetryHook(3)).       // retries first
    AddHook(hooks.NewRateLimitHook(60, 0)). // then rate limit
    AddHook(tracker).                      // then track costs
    AddHook(hooks.NewMetricsHook()).       // then record metrics
    Build()
```
