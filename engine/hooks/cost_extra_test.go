package hooks

import (
	"context"
	"testing"
)

func TestCostTracker_Before_BudgetExactlyReached(t *testing.T) {
	ct := NewCostTracker(map[string]ModelPrice{
		"m": {PromptPricePerToken: 1, CompletionPricePerToken: 0},
	})
	ct.Budget = 100
	ctx := context.Background()
	ct.After(ctx, &Event{
		Type: EventModelCallAfter,
		Name: "m",
		Metadata: map[string]any{
			"prompt_tokens": 100,
		},
	})
	err := ct.Before(ctx, &Event{Type: EventModelCallBefore, Name: "m"})
	if err == nil {
		t.Fatal("expected budget error when spend equals budget")
	}
}

func TestCostTracker_Before_NonBudgetEventIgnored(t *testing.T) {
	ct := NewCostTracker(nil)
	ct.Budget = 0.01
	ctx := context.Background()
	ct.After(ctx, &Event{
		Type: EventModelCallAfter,
		Name: "m",
		Metadata: map[string]any{
			"prompt_tokens": 100,
		},
	})
	err := ct.Before(ctx, &Event{Type: EventModelCallAfter, Name: "m"})
	if err != nil {
		t.Errorf("Before should ignore non-EventModelCallBefore: %v", err)
	}
}

func TestExtractUsage_PrefersMetadataOverOutput(t *testing.T) {
	ct := NewCostTracker(map[string]ModelPrice{
		"m": {PromptPricePerToken: 1, CompletionPricePerToken: 1},
	})
	ctx := context.Background()
	evt := &Event{
		Type:   EventModelCallAfter,
		Name:   "m",
		Output: &usageOutput{prompt: 999, completion: 999},
		Metadata: map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 5,
		},
	}
	ct.After(ctx, evt)
	g := ct.GetGlobalCost()
	if g.PromptTokens != 10 || g.CompletionTokens != 5 {
		t.Errorf("expected metadata tokens 10+5, got %d+%d", g.PromptTokens, g.CompletionTokens)
	}
}
