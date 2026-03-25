package evals

import (
	"context"
	"testing"
	"time"
)

func TestPerformanceEval_Run_NilRunFunc_Max(t *testing.T) {
	e := &PerformanceEval{EvalName: "x"}
	r := e.Run(context.Background(), "", "")
	if r.Passed || r.Error == "" {
		t.Fatalf("expected failure with message, got %+v", r)
	}
}

func TestPerformanceEval_Run_BaselineViolations_Max(t *testing.T) {
	e := &PerformanceEval{
		EvalName: "lat",
		RunFunc: func(context.Context) (time.Duration, int, int, error) {
			return 500 * time.Millisecond, 100, 100, nil
		},
		Baseline: &PerformanceBaseline{
			MaxLatency:      1 * time.Millisecond,
			MaxTotalTokens:  50,
			MaxPromptTokens: 40,
		},
	}
	r := e.Run(context.Background(), "", "")
	if r.Passed {
		t.Fatal("expected failed eval due to baseline")
	}
	if r.Score >= 1.0 {
		t.Fatalf("expected degraded score from baseline violations, got %f", r.Score)
	}
}

func TestPerformanceEval_Run_RunFuncError_Max(t *testing.T) {
	e := &PerformanceEval{
		EvalName: "err",
		RunFunc: func(context.Context) (time.Duration, int, int, error) {
			return 0, 0, 0, context.Canceled
		},
	}
	r := e.Run(context.Background(), "", "")
	if r.Passed || r.Error == "" {
		t.Fatalf("expected error result, got %+v", r)
	}
}
