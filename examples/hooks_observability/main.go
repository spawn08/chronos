// Example: hooks_observability demonstrates all Chronos hooks for production observability.
//
// This shows metrics tracking, cost estimation, rate limiting, caching, retry logic,
// and structured logging — all composable via the hook chain.
//
// No API keys needed — runs entirely with a mock provider.
//
//	go run ./examples/hooks_observability/
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/spawn08/chronos/engine/hooks"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
)

func main() {
	ctx := context.Background()

	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║    Chronos Hooks & Observability Example             ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	// ── 1. Create individual hooks ──

	metricsHook := hooks.NewMetricsHook()

	costTracker := hooks.NewCostTracker(map[string]hooks.ModelPrice{
		"mock-v1": {PromptPricePerToken: 0.000003, CompletionPricePerToken: 0.000012},
	})
	costTracker.Budget = 0.50

	cacheHook := hooks.NewCacheHook(30 * time.Second)
	cacheHook.MaxEntries = 100

	rateLimiter := hooks.NewRateLimitHook(60, 0)

	retryHook := hooks.NewRetryHook(3)
	retryHook.BaseDelay = time.Millisecond
	retryHook.OnRetry = func(attempt int, delay time.Duration) {
		fmt.Printf("  [RETRY] Attempt %d after %v\n", attempt, delay)
	}

	loggingHook := &hooks.LoggingHook{}

	// ── 2. Build agent with all hooks attached ──

	provider := &observabilityMockProvider{callCount: 0}

	a, err := agent.New("hooks-demo", "Hooks Demo Agent").
		WithModel(provider).
		WithSystemPrompt("You are a helpful assistant.").
		AddHook(loggingHook).
		AddHook(metricsHook).
		AddHook(costTracker).
		AddHook(rateLimiter).
		AddHook(cacheHook).
		AddHook(retryHook).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	// ── 3. Make several calls to exercise hooks ──

	fmt.Println("\n━━━ Making 5 chat calls ━━━")

	prompts := []string{
		"What is Go?",
		"Explain concurrency in Go.",
		"What is Go?",
		"How do channels work?",
		"What is Go?",
	}

	for i, prompt := range prompts {
		fmt.Printf("\n  Call %d: %q\n", i+1, prompt)
		resp, err := a.Chat(ctx, prompt)
		if err != nil {
			fmt.Printf("  Error: %v\n", err)
			continue
		}
		fmt.Printf("  Response: %.60s\n", resp.Content)
	}

	// ── 4. Display metrics ──

	fmt.Println("\n━━━ Metrics Summary ━━━")
	summary := metricsHook.GetSummary()
	fmt.Printf("  Total model calls:   %d\n", summary.TotalModelCalls)
	fmt.Printf("  Total tool calls:    %d\n", summary.TotalToolCalls)
	fmt.Printf("  Total errors:        %d\n", summary.TotalErrors)
	fmt.Printf("  Avg model latency:   %v\n", summary.AvgModelLatency)
	fmt.Printf("  Max model latency:   %v\n", summary.MaxModelLatency)

	fmt.Println("\n  Per-call metrics:")
	for i, m := range metricsHook.GetMetrics() {
		fmt.Printf("    [%d] type=%s  name=%-8s  duration=%-12v  error=%v\n",
			i, m.Type, m.Name, m.Duration, m.Error)
	}

	// ── 5. Display cost report ──

	fmt.Println("\n━━━ Cost Report ━━━")
	costReport := costTracker.GetGlobalCost()
	fmt.Printf("  Prompt tokens:     %d\n", costReport.PromptTokens)
	fmt.Printf("  Completion tokens: %d\n", costReport.CompletionTokens)
	fmt.Printf("  Total cost:        $%.6f %s\n", costReport.TotalCost, costReport.Currency)
	fmt.Printf("  Budget remaining:  $%.6f\n", costTracker.Budget-costReport.TotalCost)

	// ── 6. Cache statistics ──

	fmt.Println("\n━━━ Cache Statistics ━━━")
	hits, misses := cacheHook.Stats()
	fmt.Printf("  Cache hits:   %d\n", hits)
	fmt.Printf("  Cache misses: %d\n", misses)

	fmt.Println("\n✓ Hooks & Observability example completed.")
}

// observabilityMockProvider simulates realistic provider behavior including
// token usage reporting for the cost tracker.
type observabilityMockProvider struct {
	callCount int
}

func (p *observabilityMockProvider) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	p.callCount++

	last := req.Messages[len(req.Messages)-1].Content
	promptTokens := len(last) / 4
	completionTokens := promptTokens / 2

	return &model.ChatResponse{
		Content:    fmt.Sprintf("[Mock #%d] Response to: %.60s", p.callCount, last),
		Role:       "assistant",
		StopReason: model.StopReasonEnd,
		Usage: model.Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
		},
	}, nil
}

func (p *observabilityMockProvider) StreamChat(_ context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	ch := make(chan *model.ChatResponse, 1)
	resp, _ := p.Chat(context.Background(), req)
	ch <- resp
	close(ch)
	return ch, nil
}

func (p *observabilityMockProvider) Name() string  { return "mock" }
func (p *observabilityMockProvider) Model() string { return "mock-v1" }
