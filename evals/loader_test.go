package evals

import (
	"context"
	"strings"
	"testing"
)

func TestLoadSuite(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantErr   string
		wantCases int
	}{
		{
			name: "valid mixed suite",
			yaml: `
name: greeting
cases:
  - eval: exact_match
    name: exact
    input: "hello"
    expected: "hello"
  - eval: contains
    input: "the quick brown fox"
    expected: "quick"
  - eval: accuracy
    input: "The Answer"
    expected: "the answer"
`,
			wantCases: 3,
		},
		{
			name:    "missing name",
			yaml:    "cases:\n  - eval: contains\n    input: a\n    expected: a\n",
			wantErr: "missing a name",
		},
		{
			name:    "no cases",
			yaml:    "name: empty\n",
			wantErr: "no cases",
		},
		{
			name:    "unknown eval type",
			yaml:    "name: s\ncases:\n  - eval: bogus\n    input: a\n    expected: a\n",
			wantErr: "unknown eval type",
		},
		{
			name:    "malformed yaml",
			yaml:    "name: [unclosed",
			wantErr: "parse suite yaml",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			suite, err := LoadSuite([]byte(tc.yaml))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadSuite: %v", err)
			}
			if len(suite.Evals) != tc.wantCases {
				t.Errorf("cases = %d, want %d", len(suite.Evals), tc.wantCases)
			}
		})
	}
}

func TestLoadSuite_DefaultsNameToType(t *testing.T) {
	suite, err := LoadSuite([]byte("name: s\ncases:\n  - eval: contains\n    input: abc\n    expected: b\n"))
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	if got := suite.Evals[0].Eval.Name(); got != "contains" {
		t.Errorf("eval name = %q, want defaulted to type %q", got, "contains")
	}
}

func TestLoadedSuiteRuns(t *testing.T) {
	suite, err := LoadSuite([]byte(`
name: run-check
cases:
  - eval: exact_match
    input: "same"
    expected: "same"
  - eval: contains
    input: "hello world"
    expected: "zzz"
`))
	if err != nil {
		t.Fatalf("LoadSuite: %v", err)
	}
	res := suite.Run(context.Background())
	if res.Passed != 1 || res.Failed != 1 {
		t.Errorf("passed=%d failed=%d, want 1/1", res.Passed, res.Failed)
	}
}
