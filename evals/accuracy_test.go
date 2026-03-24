package evals

import (
	"context"
	"testing"
)

func TestAccuracyEval_FallbackExactMatch(t *testing.T) {
	e := &AccuracyEval{EvalName: "acc-test"}
	result := e.Run(context.Background(), "Paris", "Paris")
	if result.Score != 1.0 {
		t.Errorf("exact match score=%f, want 1.0", result.Score)
	}
	if !result.Passed {
		t.Error("exact match should pass")
	}
}

func TestAccuracyEval_FallbackContains(t *testing.T) {
	e := &AccuracyEval{EvalName: "acc-test"}
	result := e.Run(context.Background(), "The capital of France is Paris", "paris")
	if result.Score < 0.7 {
		t.Errorf("contains score=%f, want >= 0.7", result.Score)
	}
}

func TestAccuracyEval_FallbackNoMatch(t *testing.T) {
	e := &AccuracyEval{EvalName: "acc-test"}
	result := e.Run(context.Background(), "completely different answer", "expected output here")
	if result.Score > 0.5 {
		t.Errorf("no match score=%f, should be low", result.Score)
	}
}

func TestAccuracyEval_FallbackWordOverlap(t *testing.T) {
	e := &AccuracyEval{EvalName: "acc-test"}
	result := e.Run(context.Background(), "the quick brown fox", "the brown fox")
	if result.Score < 0.9 {
		t.Errorf("high overlap score=%f, want >= 0.9", result.Score)
	}
}

func TestParseJudgeResponse(t *testing.T) {
	tests := []struct {
		input     string
		wantScore float64
	}{
		{`{"score": 0.9, "explanation": "mostly correct"}`, 0.9},
		{`no json here`, 0.5},
		{`{"score": 1.0}`, 1.0},
	}
	for _, tt := range tests {
		score, _ := parseJudgeResponse(tt.input)
		if score != tt.wantScore {
			t.Errorf("parseJudgeResponse(%q) score=%f, want %f", tt.input, score, tt.wantScore)
		}
	}
}
