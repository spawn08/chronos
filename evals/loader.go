package evals

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// SuiteSpec is the YAML representation of an eval suite. It maps a declarative
// file to a runnable Suite so evals can be defined without writing Go.
//
// Example:
//
//	name: greeting-suite
//	cases:
//	  - eval: exact_match
//	    name: exact-greeting
//	    input: "hello"
//	    expected: "hello"
//	  - eval: contains
//	    input: "the quick brown fox"
//	    expected: "quick"
type SuiteSpec struct {
	Name  string     `yaml:"name"`
	Cases []CaseSpec `yaml:"cases"`
}

// CaseSpec is the YAML representation of a single eval case.
type CaseSpec struct {
	// Eval selects the evaluator: "exact_match", "contains", or "accuracy".
	Eval string `yaml:"eval"`
	// Name is an optional label; it defaults to the Eval type when empty.
	Name     string `yaml:"name"`
	Input    string `yaml:"input"`
	Expected string `yaml:"expected"`
}

// LoadSuite parses YAML suite data into a runnable Suite. It returns an error
// for malformed YAML, a missing suite name, an empty case list, or a case that
// names an unknown or unsupported evaluator.
func LoadSuite(data []byte) (*Suite, error) {
	var spec SuiteSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parse suite yaml: %w", err)
	}
	if spec.Name == "" {
		return nil, fmt.Errorf("suite is missing a name")
	}
	if len(spec.Cases) == 0 {
		return nil, fmt.Errorf("suite %q has no cases", spec.Name)
	}

	suite := &Suite{Name: spec.Name}
	for i, c := range spec.Cases {
		name := c.Name
		if name == "" {
			name = c.Eval
		}
		eval, err := evalFromSpec(c.Eval, name)
		if err != nil {
			return nil, fmt.Errorf("case %d (%q): %w", i+1, name, err)
		}
		suite.Evals = append(suite.Evals, EvalCase{
			Eval:     eval,
			Input:    c.Input,
			Expected: c.Expected,
		})
	}
	return suite, nil
}

// evalFromSpec constructs the Eval implementation named by typ. Only the
// string-comparison evaluators are expressible in YAML today; performance and
// reliability evals require structured baselines/expectations and must be
// defined programmatically.
func evalFromSpec(typ, name string) (Eval, error) {
	switch typ {
	case "exact_match", "exact":
		return &ExactMatchEval{EvalName: name}, nil
	case "contains":
		return &ContainsEval{EvalName: name}, nil
	case "accuracy":
		return &AccuracyEval{EvalName: name}, nil
	case "":
		return nil, fmt.Errorf("missing 'eval' type")
	default:
		return nil, fmt.Errorf("unknown eval type %q (supported: exact_match, contains, accuracy)", typ)
	}
}
