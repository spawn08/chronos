package evals

import (
	"context"
	"testing"
)

func TestExactMatchEval_Pass(t *testing.T) {
	e := &ExactMatchEval{EvalName: "exact-test"}
	result := e.Run(context.Background(), "hello", "hello")
	if !result.Passed {
		t.Error("exact match should pass for equal strings")
	}
	if result.Score != 1.0 {
		t.Errorf("got score=%f, want 1.0", result.Score)
	}
	if result.Name != "exact-test" {
		t.Errorf("got name=%q, want exact-test", result.Name)
	}
}

func TestExactMatchEval_Fail(t *testing.T) {
	e := &ExactMatchEval{EvalName: "exact-test"}
	result := e.Run(context.Background(), "hello", "world")
	if result.Passed {
		t.Error("exact match should fail for different strings")
	}
	if result.Score != 0.0 {
		t.Errorf("got score=%f, want 0.0", result.Score)
	}
}

func TestContainsEval_Pass(t *testing.T) {
	e := &ContainsEval{EvalName: "contains-test"}
	result := e.Run(context.Background(), "hello world", "world")
	if !result.Passed {
		t.Error("contains should pass when substring is present")
	}
}

func TestContainsEval_Fail(t *testing.T) {
	e := &ContainsEval{EvalName: "contains-test"}
	result := e.Run(context.Background(), "hello", "xyz")
	if result.Passed {
		t.Error("contains should fail when substring is not present")
	}
}

func TestContainsEval_EmptyExpected(t *testing.T) {
	e := &ContainsEval{EvalName: "empty"}
	result := e.Run(context.Background(), "hello", "")
	if result.Passed {
		t.Error("contains should fail when expected is empty")
	}
}

func TestSuite_Run(t *testing.T) {
	suite := &Suite{
		Name: "test-suite",
		Evals: []EvalCase{
			{Eval: &ExactMatchEval{EvalName: "e1"}, Input: "a", Expected: "a"},
			{Eval: &ExactMatchEval{EvalName: "e2"}, Input: "a", Expected: "b"},
			{Eval: &ContainsEval{EvalName: "e3"}, Input: "hello world", Expected: "world"},
		},
	}

	result := suite.Run(context.Background())

	if result.TotalEvals != 3 {
		t.Errorf("got total=%d, want 3", result.TotalEvals)
	}
	if result.Passed != 2 {
		t.Errorf("got passed=%d, want 2", result.Passed)
	}
	if result.Failed != 1 {
		t.Errorf("got failed=%d, want 1", result.Failed)
	}
	if result.Errors != 0 {
		t.Errorf("got errors=%d, want 0", result.Errors)
	}
}

func TestSuiteResult_Summary(t *testing.T) {
	suite := &Suite{
		Name: "summary-test",
		Evals: []EvalCase{
			{Eval: &ExactMatchEval{EvalName: "s1"}, Input: "x", Expected: "x"},
		},
	}
	result := suite.Run(context.Background())
	summary := result.Summary()
	if summary == "" {
		t.Error("summary should not be empty")
	}
}

func TestSuite_EmptyEvals(t *testing.T) {
	suite := &Suite{Name: "empty"}
	result := suite.Run(context.Background())
	if result.TotalEvals != 0 {
		t.Errorf("got total=%d, want 0", result.TotalEvals)
	}
	if result.AvgScore != 0 {
		t.Errorf("got avg_score=%f, want 0", result.AvgScore)
	}
}
