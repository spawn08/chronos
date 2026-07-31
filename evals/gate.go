package evals

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GateConfig defines the pass/fail thresholds for an eval run. A zero value gates
// on nothing (always passes); set the fields relevant to your policy.
type GateConfig struct {
	// MinAvgScore fails the gate when the report's mean score is below it.
	MinAvgScore float64 `json:"min_avg_score,omitempty"`
	// MinPassRate fails the gate when the fraction of passing cases is below it.
	MinPassRate float64 `json:"min_pass_rate,omitempty"`
	// MaxRegression fails the gate when the mean score dropped from the baseline by
	// more than this amount (absolute). Ignored when no baseline is supplied.
	MaxRegression float64 `json:"max_regression,omitempty"`
}

// GateResult is the outcome of applying a GateConfig to a report.
type GateResult struct {
	Passed  bool     `json:"passed"`
	Reasons []string `json:"reasons,omitempty"`
}

// Gate applies cfg to report, optionally comparing against a baseline (typically
// the previous run's report) to catch regressions. It never mutates its inputs.
// A nil baseline disables the regression check.
func Gate(report, baseline *DatasetReport, cfg GateConfig) GateResult {
	res := GateResult{Passed: true}
	if report == nil {
		return GateResult{Passed: false, Reasons: []string{"no report"}}
	}

	if cfg.MinAvgScore > 0 && report.AvgScore < cfg.MinAvgScore {
		res.Passed = false
		res.Reasons = append(res.Reasons,
			fmt.Sprintf("avg score %.3f below minimum %.3f", report.AvgScore, cfg.MinAvgScore))
	}
	if cfg.MinPassRate > 0 && report.PassRate < cfg.MinPassRate {
		res.Passed = false
		res.Reasons = append(res.Reasons,
			fmt.Sprintf("pass rate %.3f below minimum %.3f", report.PassRate, cfg.MinPassRate))
	}
	if cfg.MaxRegression > 0 && baseline != nil {
		drop := baseline.AvgScore - report.AvgScore
		if drop > cfg.MaxRegression {
			res.Passed = false
			res.Reasons = append(res.Reasons,
				fmt.Sprintf("avg score regressed %.3f (%.3f→%.3f), exceeds max %.3f",
					drop, baseline.AvgScore, report.AvgScore, cfg.MaxRegression))
		}
	}
	return res
}

// String renders the gate result for CLI/CI output.
func (g GateResult) String() string {
	if g.Passed {
		return "GATE PASS"
	}
	return "GATE FAIL: " + strings.Join(g.Reasons, "; ")
}

// MarshalReport renders a report as indented JSON for on-disk storage.
func MarshalReport(r *DatasetReport) ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal report: %w", err)
	}
	return data, nil
}

// LoadReport parses a report from its JSON representation.
func LoadReport(data []byte) (*DatasetReport, error) {
	var r DatasetReport
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("load report: %w", err)
	}
	return &r, nil
}
