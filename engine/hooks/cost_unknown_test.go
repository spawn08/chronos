package hooks

import (
	"context"
	"testing"
)

func TestCostTracker_UnknownModel_FlaggedNotZeroBilled(t *testing.T) {
	ct := NewCostTracker(map[string]ModelPrice{
		"known": {PromptPricePerToken: 1, CompletionPricePerToken: 1},
	})
	ctx := context.Background()
	evt := &Event{
		Type: EventModelCallAfter,
		Name: "mystery-model",
		Metadata: map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 5,
		},
	}
	if err := ct.After(ctx, evt); err != nil {
		t.Fatalf("After: %v", err)
	}

	// The unknown model is flagged on the event...
	if evt.Metadata["cost_unknown_model"] != "mystery-model" {
		t.Errorf("expected cost_unknown_model flag, got %v", evt.Metadata["cost_unknown_model"])
	}
	// ...tracked in the tracker...
	if ct.UnknownModels()["mystery-model"] != 1 {
		t.Errorf("expected unknown model recorded once, got %v", ct.UnknownModels())
	}
	// ...token usage still accounted for, but no bogus cost accrued.
	g := ct.GetGlobalCost()
	if g.TotalTokens != 15 {
		t.Errorf("total tokens = %d, want 15", g.TotalTokens)
	}
	if g.TotalCost != 0 {
		t.Errorf("total cost = %v, want 0 (unknown price, not silently billed)", g.TotalCost)
	}
}

func TestCostTracker_UnknownModel_RejectMode(t *testing.T) {
	ct := NewCostTracker(map[string]ModelPrice{
		"known": {PromptPricePerToken: 1, CompletionPricePerToken: 1},
	})
	ct.RejectUnknownModels = true
	ctx := context.Background()
	err := ct.After(ctx, &Event{
		Type:     EventModelCallAfter,
		Name:     "mystery-model",
		Metadata: map[string]any{"prompt_tokens": 3},
	})
	if err == nil {
		t.Fatal("expected error for unknown model in reject mode")
		return
	}
}

func TestCostTracker_KnownModel_StillBilled(t *testing.T) {
	ct := NewCostTracker(map[string]ModelPrice{
		"known": {PromptPricePerToken: 2, CompletionPricePerToken: 3},
	})
	ctx := context.Background()
	evt := &Event{
		Type:     EventModelCallAfter,
		Name:     "known",
		Metadata: map[string]any{"prompt_tokens": 10, "completion_tokens": 10},
	}
	if err := ct.After(ctx, evt); err != nil {
		t.Fatalf("After: %v", err)
	}
	if _, flagged := evt.Metadata["cost_unknown_model"]; flagged {
		t.Error("known model should not be flagged")
	}
	if g := ct.GetGlobalCost(); g.TotalCost != 50 {
		t.Errorf("total cost = %v, want 50", g.TotalCost)
	}
}
