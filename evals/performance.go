package evals

import (
	"context"
	"fmt"
	"time"
)

// PerformanceBaseline defines expected performance thresholds.
type PerformanceBaseline struct {
	MaxLatency      time.Duration
	MaxTotalTokens  int
	MaxPromptTokens int
}

// PerformanceEval measures execution latency and token usage,
// comparing against optional baselines.
type PerformanceEval struct {
	EvalName string
	RunFunc  func(ctx context.Context) (latency time.Duration, promptTokens int, completionTokens int, err error)
	Baseline *PerformanceBaseline
}

func (e *PerformanceEval) Name() string { return e.EvalName }

func (e *PerformanceEval) Run(ctx context.Context, _, _ string) EvalResult {
	start := time.Now()

	if e.RunFunc == nil {
		return EvalResult{
			Name:    e.EvalName,
			Score:   0,
			Passed:  false,
			Error:   "no RunFunc configured",
			Latency: time.Since(start),
		}
	}

	latency, promptTokens, completionTokens, err := e.RunFunc(ctx)
	totalTokens := promptTokens + completionTokens

	if err != nil {
		return EvalResult{
			Name:       e.EvalName,
			Score:      0,
			Passed:     false,
			Error:      err.Error(),
			Latency:    latency,
			TokensUsed: totalTokens,
		}
	}

	score := 1.0
	var details []string
	passed := true

	if e.Baseline != nil {
		if e.Baseline.MaxLatency > 0 && latency > e.Baseline.MaxLatency {
			score -= 0.3
			passed = false
			details = append(details, fmt.Sprintf("latency %s exceeds baseline %s", latency, e.Baseline.MaxLatency))
		}
		if e.Baseline.MaxTotalTokens > 0 && totalTokens > e.Baseline.MaxTotalTokens {
			score -= 0.3
			passed = false
			details = append(details, fmt.Sprintf("tokens %d exceed baseline %d", totalTokens, e.Baseline.MaxTotalTokens))
		}
		if e.Baseline.MaxPromptTokens > 0 && promptTokens > e.Baseline.MaxPromptTokens {
			score -= 0.2
			details = append(details, fmt.Sprintf("prompt tokens %d exceed baseline %d", promptTokens, e.Baseline.MaxPromptTokens))
		}
	}

	if score < 0 {
		score = 0
	}

	detailStr := fmt.Sprintf("latency=%s, tokens=%d (prompt=%d, completion=%d)", latency, totalTokens, promptTokens, completionTokens)
	for _, d := range details {
		detailStr += "; " + d
	}

	return EvalResult{
		Name:       e.EvalName,
		Score:      score,
		Passed:     passed,
		Details:    detailStr,
		Latency:    latency,
		TokensUsed: totalTokens,
	}
}
