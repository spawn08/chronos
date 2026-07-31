// Example: eval_loop demonstrates the eval-driven development loop (WC-C-001):
// capture a dataset from a real run, run it against an agent target, score it,
// and gate on regressions — the trace → dataset → eval → gate cycle.
//
// It runs with NO API keys and NO network: a stored session is seeded in an
// in-memory SQLite store, a dataset is captured from it, and two deterministic
// "agents" are scored — a good one that passes the gate and a regressed one that
// the gate catches (the same non-zero signal `chronos evals gate` gives CI).
//
//	go run ./examples/eval_loop/
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/spawn08/chronos/evals"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

const sessionID = "golden-session"

func main() {
	ctx := context.Background()

	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║          Chronos Eval-Driven Loop Example             ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	store, err := sqlite.New(":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	// 1. CAPTURE — seed a "real" run and capture it into a golden dataset.
	seedSession(ctx, store)
	ds, err := evals.CaptureFromSession(ctx, store, sessionID, "capitals")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n1. Captured dataset %q with %d golden cases from session %q\n", ds.Name, len(ds.Cases), sessionID)

	evaluators := []evals.Eval{&evals.ExactMatchEval{EvalName: "exact"}}
	gateCfg := evals.GateConfig{MinAvgScore: 0.9, MaxRegression: 0.1}
	history := evals.NewMemReportStore()

	// 2 & 3. RUN + GATE — a good agent passes the gate (no baseline yet).
	fmt.Println("\n2. Running the good agent…")
	good := &evals.DatasetRunner{Target: goodAgent, Evaluators: evaluators}
	goodReport, err := good.Run(ctx, ds)
	if err != nil {
		log.Fatal(err)
	}
	gate(ctx, history, goodReport, gateCfg)

	// 4. REGRESSION — a worse agent trips the gate against the good baseline.
	fmt.Println("\n3. A change regresses the agent — running again…")
	bad := &evals.DatasetRunner{Target: regressedAgent, Evaluators: evaluators}
	badReport, err := bad.Run(ctx, ds)
	if err != nil {
		log.Fatal(err)
	}
	gate(ctx, history, badReport, gateCfg)

	fmt.Println("\n✓ Capture → dataset → run → gate. In CI, `chronos evals gate` exits")
	fmt.Println("  non-zero on the regression, blocking the merge.")
}

// gate runs the report against the previous run (baseline) and reports the
// outcome, then records the run in history for the next comparison.
func gate(ctx context.Context, history evals.ReportStore, report *evals.DatasetReport, cfg evals.GateConfig) {
	hist, _ := history.History(ctx, report.Dataset)
	baseline := evals.BaselineFrom(hist)
	result := evals.Gate(report, baseline, cfg)
	fmt.Printf("   avg_score=%.3f pass_rate=%.3f (%d/%d) → %s\n",
		report.AvgScore, report.PassRate, report.Passed, report.Total, result.String())
	_ = history.SaveReport(ctx, report)
}

// seedSession writes a short user/assistant conversation to the event ledger,
// standing in for a real agent run whose transcript we later capture.
func seedSession(ctx context.Context, store storage.Storage) {
	if err := store.CreateSession(ctx, &storage.Session{ID: sessionID, AgentID: "geo-agent", Status: "active"}); err != nil {
		log.Fatal(err)
	}
	turns := []struct{ role, content string }{
		{"user", "capital of France"}, {"assistant", "Paris"},
		{"user", "capital of Japan"}, {"assistant", "Tokyo"},
		{"user", "capital of Italy"}, {"assistant", "Rome"},
	}
	for i, tn := range turns {
		if err := store.AppendEvent(ctx, &storage.Event{
			ID:        fmt.Sprintf("evt-%d", i),
			SessionID: sessionID,
			SeqNum:    int64(i + 1),
			Type:      "chat_message",
			Payload:   map[string]any{"role": tn.role, "content": tn.content},
		}); err != nil {
			log.Fatal(err)
		}
	}
}

// goodAgent answers every captured question correctly.
func goodAgent(_ context.Context, input string) (string, error) {
	return map[string]string{
		"capital of France": "Paris",
		"capital of Japan":  "Tokyo",
		"capital of Italy":  "Rome",
	}[input], nil
}

// regressedAgent gets one answer wrong, dropping the score below the gate.
func regressedAgent(_ context.Context, input string) (string, error) {
	return map[string]string{
		"capital of France": "Paris",
		"capital of Japan":  "Kyoto", // regression
		"capital of Italy":  "Milan", // regression
	}[input], nil
}
