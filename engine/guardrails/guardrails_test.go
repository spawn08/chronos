package guardrails

import (
	"context"
	"strings"
	"testing"
)

func TestBlocklistGuardrail_Blocks(t *testing.T) {
	tests := []struct {
		name      string
		blocklist []string
		content   string
		wantPass  bool
	}{
		{"exact match", []string{"forbidden"}, "this is forbidden content", false},
		{"case insensitive", []string{"SECRET"}, "this is secret data", false},
		{"no match", []string{"forbidden"}, "this is clean content", true},
		{"empty content", []string{"forbidden"}, "", true},
		{"empty blocklist", []string{}, "anything goes", true},
		{"multiple terms first match", []string{"bad", "evil"}, "this is bad", false},
		{"multiple terms second match", []string{"bad", "evil"}, "this is evil", false},
		{"substring match", []string{"pass"}, "password123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &BlocklistGuardrail{Blocklist: tt.blocklist}
			result := g.Check(context.Background(), tt.content)
			if result.Passed != tt.wantPass {
				t.Errorf("Passed = %v, want %v (reason: %s)", result.Passed, tt.wantPass, result.Reason)
			}
			if !tt.wantPass && result.Reason == "" {
				t.Error("expected non-empty reason on block")
			}
		})
	}
}

func TestMaxLengthGuardrail(t *testing.T) {
	tests := []struct {
		name     string
		maxChars int
		content  string
		wantPass bool
	}{
		{"under limit", 100, "short", true},
		{"at limit", 5, "hello", true},
		{"over limit", 5, "hello!", false},
		{"empty content", 10, "", true},
		{"zero limit blocks all", 0, "a", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &MaxLengthGuardrail{MaxChars: tt.maxChars}
			result := g.Check(context.Background(), tt.content)
			if result.Passed != tt.wantPass {
				t.Errorf("Passed = %v, want %v", result.Passed, tt.wantPass)
			}
		})
	}
}

func TestEngine_CheckInput(t *testing.T) {
	e := NewEngine()
	e.AddRule(Rule{
		Name:      "blocklist",
		Position:  Input,
		Guardrail: &BlocklistGuardrail{Blocklist: []string{"banned"}},
	})
	e.AddRule(Rule{
		Name:      "maxlen",
		Position:  Input,
		Guardrail: &MaxLengthGuardrail{MaxChars: 50},
	})

	ctx := context.Background()

	if r := e.CheckInput(ctx, "hello world"); r != nil {
		t.Errorf("expected pass, got: %s", r.Reason)
	}

	if r := e.CheckInput(ctx, "this is banned content"); r == nil {
		t.Error("expected block from blocklist guardrail")
	}

	if r := e.CheckInput(ctx, strings.Repeat("a", 51)); r == nil {
		t.Error("expected block from max length guardrail")
	}
}

func TestEngine_CheckOutput(t *testing.T) {
	e := NewEngine()
	e.AddRule(Rule{
		Name:      "output-blocklist",
		Position:  Output,
		Guardrail: &BlocklistGuardrail{Blocklist: []string{"PII"}},
	})

	ctx := context.Background()

	if r := e.CheckOutput(ctx, "safe content"); r != nil {
		t.Errorf("expected pass, got: %s", r.Reason)
	}

	r := e.CheckOutput(ctx, "contains PII data")
	if r == nil {
		t.Fatal("expected block")
	}
	if !strings.Contains(r.Reason, "output") {
		t.Errorf("reason should indicate output position: %s", r.Reason)
	}
}

func TestEngine_InputDoesNotTriggerOutput(t *testing.T) {
	e := NewEngine()
	e.AddRule(Rule{
		Name:      "output-only",
		Position:  Output,
		Guardrail: &BlocklistGuardrail{Blocklist: []string{"blocked"}},
	})

	if r := e.CheckInput(context.Background(), "this is blocked"); r != nil {
		t.Error("output rule should not fire on input check")
	}
}

func TestEngine_EmptyEngine(t *testing.T) {
	e := NewEngine()
	if r := e.CheckInput(context.Background(), "anything"); r != nil {
		t.Error("empty engine should pass all input")
	}
	if r := e.CheckOutput(context.Background(), "anything"); r != nil {
		t.Error("empty engine should pass all output")
	}
}

func TestEngine_MultipleRules_FirstFailure(t *testing.T) {
	e := NewEngine()
	e.AddRule(Rule{
		Name:      "first",
		Position:  Input,
		Guardrail: &BlocklistGuardrail{Blocklist: []string{"alpha"}},
	})
	e.AddRule(Rule{
		Name:      "second",
		Position:  Input,
		Guardrail: &BlocklistGuardrail{Blocklist: []string{"beta"}},
	})

	r := e.CheckInput(context.Background(), "contains alpha and beta")
	if r == nil {
		t.Fatal("expected block")
	}
	if !strings.Contains(r.Reason, "first") {
		t.Errorf("should report first failing rule, got: %s", r.Reason)
	}
}
