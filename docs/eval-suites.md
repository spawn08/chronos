# Eval Suites

Chronos ships an evaluation framework (`evals/`) for scoring agent quality. Suites can be defined in Go or declared in YAML and run from the CLI.

## Running a suite from the CLI

```bash
# List suites discovered under .chronos/evals/ or evals/
chronos eval list

# Run a suite file
chronos eval run evals/examples/greeting_suite.yaml
```

The command prints a per-case result line and a summary, and exits non-zero if any case fails or errors — so it can gate CI.

```
Running suite "greeting-suite" (3 cases) from evals/examples/greeting_suite.yaml

  [PASS ] exact-greeting           score=1.00  input="hello" expected="hello" match=true
  [PASS ] mentions-quick           score=1.00  contains("the quick brown fox", "quick")=true
  [PASS ] case-insensitive         score=1.00  exact match

Suite: greeting-suite | 3/3 passed (100% avg score) | 0 errors | 0s total
```

## Suite YAML schema

```yaml
name: greeting-suite          # required
cases:                        # required, at least one
  - eval: exact_match         # evaluator type (see below)
    name: exact-greeting      # optional label; defaults to the eval type
    input: "hello"            # the value under test
    expected: "hello"         # the expected value

  - eval: contains
    input: "the quick brown fox"
    expected: "quick"

  - eval: accuracy
    input: "The Answer Is 42"
    expected: "the answer is 42"
```

### Supported evaluator types

| `eval` | Passes when |
|--------|-------------|
| `exact_match` (or `exact`) | `input == expected` |
| `contains` | `input` contains the `expected` substring |
| `accuracy` | fuzzy match (exact, case-insensitive, or substring) with a graded score |

Performance and reliability evaluators require structured baselines / tool-call expectations and are defined programmatically with the `evals` package (see `evals/performance.go`, `evals/reliability.go`).

## Defining a suite in Go

```go
import "github.com/spawn08/chronos/evals"

suite := &evals.Suite{
    Name: "greeting",
    Evals: []evals.EvalCase{
        {Eval: &evals.ExactMatchEval{EvalName: "exact"}, Input: "hi", Expected: "hi"},
        {Eval: &evals.ContainsEval{EvalName: "has-quick"}, Input: "the quick fox", Expected: "quick"},
    },
}
result := suite.Run(ctx)
fmt.Println(result.Summary())
```

Load a YAML suite programmatically with `evals.LoadSuite([]byte)`.
