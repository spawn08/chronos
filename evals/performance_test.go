package evals

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestPerformanceEval_WithinBaseline(t *testing.T) {
	e := &PerformanceEval{
		EvalName: "perf-test",
		RunFunc: func(_ context.Context) (time.Duration, int, int, error) {
			return 100 * time.Millisecond, 50, 30, nil
		},
		Baseline: &PerformanceBaseline{
			MaxLatency:     time.Second,
			MaxTotalTokens: 200,
		},
	}
	result := e.Run(context.Background(), "", "")
	if result.Score != 1.0 {
		t.Errorf("within baseline score=%f, want 1.0", result.Score)
	}
	if !result.Passed {
		t.Error("within baseline should pass")
	}
}

func TestPerformanceEval_ExceedsLatency(t *testing.T) {
	e := &PerformanceEval{
		EvalName: "perf-test",
		RunFunc: func(_ context.Context) (time.Duration, int, int, error) {
			return 2 * time.Second, 50, 30, nil
		},
		Baseline: &PerformanceBaseline{
			MaxLatency: time.Second,
		},
	}
	result := e.Run(context.Background(), "", "")
	if result.Score >= 1.0 {
		t.Errorf("exceeds latency score=%f, should be < 1.0", result.Score)
	}
	if result.Passed {
		t.Error("exceeds latency should not pass")
	}
}

func TestPerformanceEval_ExceedsTokens(t *testing.T) {
	e := &PerformanceEval{
		EvalName: "perf-test",
		RunFunc: func(_ context.Context) (time.Duration, int, int, error) {
			return 100 * time.Millisecond, 500, 300, nil
		},
		Baseline: &PerformanceBaseline{
			MaxTotalTokens: 100,
		},
	}
	result := e.Run(context.Background(), "", "")
	if result.Passed {
		t.Error("exceeds tokens should not pass")
	}
}

func TestPerformanceEval_RunFuncError(t *testing.T) {
	e := &PerformanceEval{
		EvalName: "perf-test",
		RunFunc: func(_ context.Context) (time.Duration, int, int, error) {
			return 0, 0, 0, fmt.Errorf("connection failed")
		},
	}
	result := e.Run(context.Background(), "", "")
	if result.Error == "" {
		t.Error("error should be reported")
	}
	if result.Passed {
		t.Error("error run should not pass")
	}
}

func TestPerformanceEval_NoRunFunc(t *testing.T) {
	e := &PerformanceEval{EvalName: "perf-test"}
	result := e.Run(context.Background(), "", "")
	if result.Error == "" {
		t.Error("missing RunFunc should report error")
	}
}

func TestPerformanceEval_NoBaseline(t *testing.T) {
	e := &PerformanceEval{
		EvalName: "perf-test",
		RunFunc: func(_ context.Context) (time.Duration, int, int, error) {
			return 100 * time.Millisecond, 50, 30, nil
		},
	}
	result := e.Run(context.Background(), "", "")
	if result.Score != 1.0 {
		t.Errorf("no baseline score=%f, want 1.0", result.Score)
	}
}

func TestPerformanceEval_Name(t *testing.T) {
	e := &PerformanceEval{EvalName: "perf"}
	if e.Name() != "perf" {
		t.Errorf("Name=%q", e.Name())
	}
}
