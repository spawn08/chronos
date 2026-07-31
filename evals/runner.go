package evals

import (
	"context"
	"fmt"
	"time"
)

// Target is the system under evaluation: given a case input it returns the
// agent's (or graph's) actual output. It abstracts sdk/agent and engine/graph so
// the runner depends on neither — pass e.g. a closure over agent.Chat.
type Target func(ctx context.Context, input string) (string, error)

// DatasetRunner executes a Dataset against a Target and scores each produced
// output with one or more Evaluators. It is the "run" stage of the eval loop.
type DatasetRunner struct {
	Target     Target
	Evaluators []Eval
}

// CaseReport is the outcome for a single dataset case: the actual output the
// target produced and each evaluator's score against the expected output.
type CaseReport struct {
	Input    string       `json:"input"`
	Expected string       `json:"expected"`
	Actual   string       `json:"actual"`
	Error    string       `json:"error,omitempty"`
	Results  []EvalResult `json:"results"`
	Score    float64      `json:"score"`  // mean evaluator score for this case
	Passed   bool         `json:"passed"` // all evaluators passed and the target did not error
}

// DatasetReport aggregates a run of a Dataset: per-case outcomes plus the summary
// metrics the gate compares (mean score and pass rate).
type DatasetReport struct {
	Dataset  string       `json:"dataset"`
	RanAt    time.Time    `json:"ran_at"`
	Cases    []CaseReport `json:"cases"`
	Total    int          `json:"total"`
	Passed   int          `json:"passed"`
	AvgScore float64      `json:"avg_score"`
	PassRate float64      `json:"pass_rate"`
}

// Run executes every case in ds against the target, scoring with the configured
// evaluators, and returns the aggregated report. A target error fails that case
// (recorded, score 0) without aborting the run. It returns an error only for a
// misconfigured runner (no target or no evaluators).
func (r *DatasetRunner) Run(ctx context.Context, ds *Dataset) (*DatasetReport, error) {
	if r.Target == nil {
		return nil, fmt.Errorf("eval run: nil target")
	}
	if len(r.Evaluators) == 0 {
		return nil, fmt.Errorf("eval run: no evaluators")
	}
	if ds == nil {
		return nil, fmt.Errorf("eval run: nil dataset")
	}

	report := &DatasetReport{Dataset: ds.Name, RanAt: time.Now(), Total: len(ds.Cases)}
	var scoreSum float64
	for _, c := range ds.Cases {
		cr := r.runCase(ctx, c)
		report.Cases = append(report.Cases, cr)
		scoreSum += cr.Score
		if cr.Passed {
			report.Passed++
		}
	}
	if report.Total > 0 {
		report.AvgScore = scoreSum / float64(report.Total)
		report.PassRate = float64(report.Passed) / float64(report.Total)
	}
	return report, nil
}

// runCase produces the target output for one case and scores it with every
// evaluator. A case passes only if the target succeeded and every evaluator
// passed; its Score is the mean evaluator score.
func (r *DatasetRunner) runCase(ctx context.Context, c DatasetCase) CaseReport {
	cr := CaseReport{Input: c.Input, Expected: c.Expected}

	actual, err := r.Target(ctx, c.Input)
	if err != nil {
		cr.Error = err.Error()
		return cr
	}
	cr.Actual = actual

	var scoreSum float64
	allPassed := true
	for _, e := range r.Evaluators {
		res := e.Run(ctx, actual, c.Expected)
		cr.Results = append(cr.Results, res)
		scoreSum += res.Score
		if res.Error != "" || !res.Passed {
			allPassed = false
		}
	}
	// Run guarantees at least one evaluator before any case executes.
	cr.Score = scoreSum / float64(len(r.Evaluators))
	cr.Passed = allPassed
	return cr
}
