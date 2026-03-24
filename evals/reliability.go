package evals

import (
	"context"
	"fmt"
	"time"
)

// ToolCallExpectation defines an expected tool call.
type ToolCallExpectation struct {
	Name string
	Args map[string]any
}

// ReliabilityEval verifies the agent made correct tool calls in the correct order.
type ReliabilityEval struct {
	EvalName      string
	ActualCalls   []ToolCallExpectation
	ExpectedCalls []ToolCallExpectation
}

func (e *ReliabilityEval) Name() string { return e.EvalName }

func (e *ReliabilityEval) Run(_ context.Context, _, _ string) EvalResult {
	start := time.Now()

	if len(e.ExpectedCalls) == 0 {
		return EvalResult{
			Name:    e.EvalName,
			Score:   1.0,
			Passed:  true,
			Details: "no expected tool calls",
			Latency: time.Since(start),
		}
	}

	matched := 0
	total := len(e.ExpectedCalls)

	for i, expected := range e.ExpectedCalls {
		if i >= len(e.ActualCalls) {
			break
		}
		actual := e.ActualCalls[i]
		if actual.Name == expected.Name {
			if argsMatch(actual.Args, expected.Args) {
				matched++
			}
		}
	}

	score := float64(matched) / float64(total)
	return EvalResult{
		Name:    e.EvalName,
		Score:   score,
		Passed:  score >= 0.8,
		Details: fmt.Sprintf("matched %d/%d tool calls", matched, total),
		Latency: time.Since(start),
	}
}

func argsMatch(actual, expected map[string]any) bool {
	if len(expected) == 0 {
		return true
	}
	for k, v := range expected {
		av, ok := actual[k]
		if !ok {
			return false
		}
		if fmt.Sprintf("%v", av) != fmt.Sprintf("%v", v) {
			return false
		}
	}
	return true
}
