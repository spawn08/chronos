package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// writeReport writes a minimal report JSON to a temp file and returns its path.
func writeReport(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEvalGate_PassAndFail(t *testing.T) {
	good := writeReport(t, `{"dataset":"d","avg_score":0.95,"pass_rate":1.0,"passed":5,"total":5}`)
	bad := writeReport(t, `{"dataset":"d","avg_score":0.55,"pass_rate":0.4,"passed":2,"total":5}`)

	if err := evalGate([]string{good, "--min-score", "0.8"}); err != nil {
		t.Errorf("healthy report should pass the gate, got %v", err)
	}
	if err := evalGate([]string{bad, "--min-score", "0.8"}); err == nil {
		t.Error("regressed report should fail the gate (non-nil error)")
	}
}

func TestEvalGate_RegressionAgainstBaseline(t *testing.T) {
	baseline := writeReport(t, `{"dataset":"d","avg_score":0.95}`)
	regressed := writeReport(t, `{"dataset":"d","avg_score":0.70}`)

	err := evalGate([]string{regressed, "--baseline", baseline, "--max-regression", "0.1"})
	if err == nil {
		t.Error("a 0.25 drop should trip the max-regression gate")
	}
}

func TestEvalGate_Usage(t *testing.T) {
	if err := evalGate(nil); err == nil {
		t.Error("expected usage error with no args")
	}
}
