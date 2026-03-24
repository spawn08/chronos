// Package evals provides an evaluation framework for testing agent quality.
package evals

import (
	"context"
	"fmt"
	"time"
)

// Eval defines a single evaluation case.
type Eval interface {
	// Name returns a human-readable name for this eval.
	Name() string
	// Run executes the evaluation and returns a result.
	Run(ctx context.Context, input string, expected string) EvalResult
}

// EvalResult captures the outcome of a single evaluation.
type EvalResult struct {
	Name       string        `json:"name"`
	Score      float64       `json:"score"`
	Passed     bool          `json:"passed"`
	Details    string        `json:"details"`
	Latency    time.Duration `json:"latency"`
	TokensUsed int           `json:"tokens_used,omitempty"`
	Error      string        `json:"error,omitempty"`
}

// Suite is a collection of evaluations that run together.
type Suite struct {
	Name  string
	Evals []EvalCase
}

// EvalCase pairs an Eval with test data.
type EvalCase struct {
	Eval     Eval
	Input    string
	Expected string
}

// SuiteResult aggregates results from running a suite.
type SuiteResult struct {
	SuiteName    string        `json:"suite_name"`
	Results      []EvalResult  `json:"results"`
	TotalEvals   int           `json:"total_evals"`
	Passed       int           `json:"passed"`
	Failed       int           `json:"failed"`
	Errors       int           `json:"errors"`
	AvgScore     float64       `json:"avg_score"`
	TotalLatency time.Duration `json:"total_latency"`
}

// Run executes all evaluations in the suite and aggregates results.
func (s *Suite) Run(ctx context.Context) SuiteResult {
	sr := SuiteResult{
		SuiteName:  s.Name,
		TotalEvals: len(s.Evals),
	}

	var totalScore float64
	for _, ec := range s.Evals {
		result := ec.Eval.Run(ctx, ec.Input, ec.Expected)
		sr.Results = append(sr.Results, result)
		sr.TotalLatency += result.Latency
		totalScore += result.Score

		if result.Error != "" {
			sr.Errors++
		} else if result.Passed {
			sr.Passed++
		} else {
			sr.Failed++
		}
	}

	if sr.TotalEvals > 0 {
		sr.AvgScore = totalScore / float64(sr.TotalEvals)
	}

	return sr
}

// Summary returns a human-readable summary of the suite results.
func (sr *SuiteResult) Summary() string {
	return fmt.Sprintf(
		"Suite: %s | %d/%d passed (%.0f%% avg score) | %d errors | %s total",
		sr.SuiteName,
		sr.Passed, sr.TotalEvals,
		sr.AvgScore*100,
		sr.Errors,
		sr.TotalLatency.Round(time.Millisecond),
	)
}

// ExactMatchEval checks if the agent output exactly matches the expected output.
type ExactMatchEval struct {
	EvalName string
}

func (e *ExactMatchEval) Name() string { return e.EvalName }

func (e *ExactMatchEval) Run(_ context.Context, input, expected string) EvalResult {
	start := time.Now()
	passed := input == expected
	score := 0.0
	if passed {
		score = 1.0
	}
	return EvalResult{
		Name:    e.EvalName,
		Score:   score,
		Passed:  passed,
		Details: fmt.Sprintf("input=%q expected=%q match=%v", input, expected, passed),
		Latency: time.Since(start),
	}
}

// ContainsEval checks if the input contains the expected substring.
type ContainsEval struct {
	EvalName string
}

func (e *ContainsEval) Name() string { return e.EvalName }

func (e *ContainsEval) Run(_ context.Context, input, expected string) EvalResult {
	start := time.Now()
	passed := len(expected) > 0 && contains(input, expected)
	score := 0.0
	if passed {
		score = 1.0
	}
	return EvalResult{
		Name:    e.EvalName,
		Score:   score,
		Passed:  passed,
		Details: fmt.Sprintf("contains(%q, %q)=%v", input, expected, passed),
		Latency: time.Since(start),
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
