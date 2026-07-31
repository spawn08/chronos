---
title: "Eval-Driven Loop"
sidebar_label: "Eval Loop"
---


Shipping agent quality is a loop, not a one-off test: **capture** real runs into a
golden dataset, **run** that dataset against your agent, **score** the outputs, and
**gate** merges when scores regress. Chronos builds this loop on top of the
existing `evals` package (evaluators + LLM-as-judge).

```
trace/run ──► dataset ──► run vs target ──► score ──► gate (CI)
   capture       curate        DatasetRunner    Eval      Gate
```

## 1. Capture — traces → dataset

Turn a stored session's conversation into a golden dataset (each user turn → an
input, the assistant reply → the expected output):

```go
ds, err := evals.CaptureFromSession(ctx, store, sessionID, "capitals")
data, _ := evals.MarshalDataset(ds) // JSON for on-disk storage
```

Or from the CLI:

```bash
chronos evals capture <sessionID> --name capitals --out capitals.json
```

Capture reads the append-only event ledger and is tenant-scoped, so you only ever
capture your own sessions.

## 2. Run — dataset → scored report

Run the dataset against a **target** (any `func(ctx, input) (string, error)` — wrap
`agent.Chat`, a graph, or a remote agent) and score each output with one or more
evaluators:

```go
runner := &evals.DatasetRunner{
    Target: func(ctx context.Context, input string) (string, error) {
        resp, err := myAgent.Chat(ctx, input)
        if err != nil { return "", err }
        return resp.Content, nil
    },
    Evaluators: []evals.Eval{
        &evals.ExactMatchEval{EvalName: "exact"},
        &evals.AccuracyEval{EvalName: "judge", Judge: judgeModel}, // LLM-as-judge
    },
}
report, _ := runner.Run(ctx, ds)
fmt.Printf("avg_score=%.3f pass_rate=%.3f\n", report.AvgScore, report.PassRate)
```

Available evaluators: `ExactMatchEval`, `ContainsEval`, `AccuracyEval`
(LLM-as-judge, falls back to word-overlap without a judge), `PerformanceEval`,
`ReliabilityEval`.

## 3. Gate — block regressions

A `Gate` compares a report against thresholds and, optionally, a baseline
(typically the previous run) to catch regressions:

```go
result := evals.Gate(report, baseline, evals.GateConfig{
    MinAvgScore:   0.9,
    MinPassRate:   0.9,
    MaxRegression: 0.05, // fail if avg score drops >0.05 from baseline
})
if !result.Passed {
    log.Fatal(result.String()) // non-zero exit blocks CI
}
```

### In CI

Produce a report from your run, write it as JSON, then gate it:

```bash
chronos evals gate report.json --min-score 0.9 --baseline last-report.json --max-regression 0.05
```

The command exits non-zero when the gate fails, so it blocks the merge. Chronos's
own CI has an **Eval Gate** job that runs the loop end-to-end and asserts a
regressed report is rejected.

## Trend history

Scores are queryable over time via a `ReportStore`, scoped to the tenant. Save each
run and use the most recent as the next run's baseline:

```go
history := evals.NewStorageReportStore(store) // or NewMemReportStore()
past, _ := history.History(ctx, ds.Name)
baseline := evals.BaselineFrom(past) // nil on the first run
// … run + gate against baseline …
_ = history.SaveReport(ctx, report)
```

`StorageReportStore` records each run as an append-only checkpoint keyed by a
tenant-scoped, per-dataset session id, so runs never overwrite one another and one
tenant never sees another's history. Inspect it from the CLI:

```bash
chronos evals history <dataset>
```

## Complete example

See [`examples/eval_loop/`](https://github.com/spawn08/chronos/tree/main/examples/eval_loop)
for a runnable, key-free demonstration that captures a dataset from a seeded
session, runs a good agent (gate passes) and a regressed agent (gate fails).
