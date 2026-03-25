package evals

import (
	"context"
	"testing"
)

func TestReliabilityEval_AllMatch(t *testing.T) {
	e := &ReliabilityEval{
		EvalName: "rel-test",
		ExpectedCalls: []ToolCallExpectation{
			{Name: "search", Args: map[string]any{"query": "go"}},
			{Name: "read", Args: map[string]any{"file": "main.go"}},
		},
		ActualCalls: []ToolCallExpectation{
			{Name: "search", Args: map[string]any{"query": "go"}},
			{Name: "read", Args: map[string]any{"file": "main.go"}},
		},
	}
	result := e.Run(context.Background(), "", "")
	if result.Score != 1.0 {
		t.Errorf("all match score=%f, want 1.0", result.Score)
	}
}

func TestReliabilityEval_PartialMatch(t *testing.T) {
	e := &ReliabilityEval{
		EvalName: "rel-test",
		ExpectedCalls: []ToolCallExpectation{
			{Name: "search", Args: map[string]any{"query": "go"}},
			{Name: "read", Args: map[string]any{"file": "main.go"}},
		},
		ActualCalls: []ToolCallExpectation{
			{Name: "search", Args: map[string]any{"query": "go"}},
			{Name: "write", Args: map[string]any{"file": "main.go"}},
		},
	}
	result := e.Run(context.Background(), "", "")
	if result.Score != 0.5 {
		t.Errorf("partial match score=%f, want 0.5", result.Score)
	}
}

func TestReliabilityEval_NoExpected(t *testing.T) {
	e := &ReliabilityEval{EvalName: "rel-test"}
	result := e.Run(context.Background(), "", "")
	if result.Score != 1.0 {
		t.Errorf("no expected score=%f, want 1.0", result.Score)
	}
}

func TestReliabilityEval_MissingActual(t *testing.T) {
	e := &ReliabilityEval{
		EvalName: "rel-test",
		ExpectedCalls: []ToolCallExpectation{
			{Name: "search"},
			{Name: "read"},
		},
		ActualCalls: []ToolCallExpectation{
			{Name: "search"},
		},
	}
	result := e.Run(context.Background(), "", "")
	if result.Score != 0.5 {
		t.Errorf("missing actual score=%f, want 0.5", result.Score)
	}
}

func TestArgsMatch(t *testing.T) {
	if !argsMatch(map[string]any{"a": 1, "b": 2}, map[string]any{"a": 1}) {
		t.Error("subset match should pass")
	}
	if argsMatch(map[string]any{"a": 1}, map[string]any{"a": 2}) {
		t.Error("different values should fail")
	}
	if !argsMatch(nil, nil) {
		t.Error("both nil should match")
	}
}

func TestReliabilityEval_Name(t *testing.T) {
	e := &ReliabilityEval{EvalName: "rel"}
	if e.Name() != "rel" {
		t.Errorf("Name=%q", e.Name())
	}
}
