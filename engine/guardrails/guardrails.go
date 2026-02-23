// Package guardrails provides input/output validation for agent execution.
package guardrails

import (
	"context"
	"fmt"
	"strings"
)

// Result is the outcome of a guardrail check.
type Result struct {
	Passed  bool   `json:"passed"`
	Reason  string `json:"reason,omitempty"`
}

// Guardrail validates input or output at runtime.
type Guardrail interface {
	// Check validates the content. Returns passed=false with a reason to block.
	Check(ctx context.Context, content string) Result
}

// Position indicates where a guardrail applies.
type Position string

const (
	Input  Position = "input"
	Output Position = "output"
)

// Rule combines a guardrail with its position.
type Rule struct {
	Name      string
	Position  Position
	Guardrail Guardrail
}

// Engine runs guardrail checks.
type Engine struct {
	rules []Rule
}

func NewEngine() *Engine {
	return &Engine{}
}

func (e *Engine) AddRule(r Rule) {
	e.rules = append(e.rules, r)
}

// CheckInput runs all input guardrails. Returns the first failure, or nil.
func (e *Engine) CheckInput(ctx context.Context, content string) *Result {
	return e.check(ctx, content, Input)
}

// CheckOutput runs all output guardrails. Returns the first failure, or nil.
func (e *Engine) CheckOutput(ctx context.Context, content string) *Result {
	return e.check(ctx, content, Output)
}

func (e *Engine) check(ctx context.Context, content string, pos Position) *Result {
	for _, r := range e.rules {
		if r.Position != pos {
			continue
		}
		result := r.Guardrail.Check(ctx, content)
		if !result.Passed {
			return &Result{
				Passed: false,
				Reason: fmt.Sprintf("[%s] %s: %s", r.Position, r.Name, result.Reason),
			}
		}
	}
	return nil
}

// --- Built-in guardrails ---

// BlocklistGuardrail rejects content containing any blocked term.
type BlocklistGuardrail struct {
	Blocklist []string
}

func (g *BlocklistGuardrail) Check(_ context.Context, content string) Result {
	lower := strings.ToLower(content)
	for _, term := range g.Blocklist {
		if strings.Contains(lower, strings.ToLower(term)) {
			return Result{Passed: false, Reason: fmt.Sprintf("blocked term: %q", term)}
		}
	}
	return Result{Passed: true}
}

// MaxLengthGuardrail rejects content exceeding a character limit.
type MaxLengthGuardrail struct {
	MaxChars int
}

func (g *MaxLengthGuardrail) Check(_ context.Context, content string) Result {
	if len(content) > g.MaxChars {
		return Result{Passed: false, Reason: fmt.Sprintf("content exceeds %d characters", g.MaxChars)}
	}
	return Result{Passed: true}
}
