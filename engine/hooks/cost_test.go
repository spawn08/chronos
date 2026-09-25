package hooks

import (
	"context"
	"fmt"
	"testing"

	"github.com/spawn08/chronos/engine/model"
)

func TestNewCostTracker(t *testing.T) {
	ct := NewCostTracker(nil)
	if ct == nil {
		t.Fatal("NewCostTracker returned nil")
		return
	}
	report := ct.GetGlobalCost()
	if report.Currency != "USD" {
		t.Errorf("expected USD, got %s", report.Currency)
	}
}

func TestCostTrackerAccumulatesTokens(t *testing.T) {
	ct := NewCostTracker(map[string]ModelPrice{
		"gpt-4o": {PromptPricePerToken: 0.01, CompletionPricePerToken: 0.02},
	})
	ctx := context.Background()

	evt := &Event{
		Type:  EventModelCallAfter,
		Name:  "gpt-4o",
		Input: "hello",
		Metadata: map[string]any{
			"prompt_tokens":     100,
			"completion_tokens": 50,
		},
	}
	if err := ct.After(ctx, evt); err != nil {
		t.Fatalf("After failed: %v", err)
	}

	report := ct.GetGlobalCost()
	if report.PromptTokens != 100 {
		t.Errorf("expected 100 prompt tokens, got %d", report.PromptTokens)
	}
	if report.CompletionTokens != 50 {
		t.Errorf("expected 50 completion tokens, got %d", report.CompletionTokens)
	}
	if report.TotalTokens != 150 {
		t.Errorf("expected 150 total tokens, got %d", report.TotalTokens)
	}
	expectedCost := 100*0.01 + 50*0.02
	if report.TotalCost != expectedCost {
		t.Errorf("expected cost %.4f, got %.4f", expectedCost, report.TotalCost)
	}
}

func TestCostTrackerSessionCost(t *testing.T) {
	ct := NewCostTracker(map[string]ModelPrice{
		"claude-3-haiku": {PromptPricePerToken: 0.001, CompletionPricePerToken: 0.002},
	})
	ctx := context.Background()

	evt := &Event{
		Type:  EventModelCallAfter,
		Name:  "claude-3-haiku",
		Input: "q",
		Metadata: map[string]any{
			"prompt_tokens":     200,
			"completion_tokens": 100,
			"session_id":        "sess-1",
		},
	}
	ct.After(ctx, evt)

	sess := ct.GetSessionCost("sess-1")
	if sess.PromptTokens != 200 {
		t.Errorf("session prompt tokens: expected 200, got %d", sess.PromptTokens)
	}
	if sess.Currency != "USD" {
		t.Errorf("expected USD currency, got %s", sess.Currency)
	}

	other := ct.GetSessionCost("sess-2")
	if other.TotalCost != 0 {
		t.Errorf("expected zero cost for unknown session")
	}
}

func TestCostTrackerSkipsErrors(t *testing.T) {
	ct := NewCostTracker(nil)
	ctx := context.Background()
	evt := &Event{
		Type:  EventModelCallAfter,
		Name:  "gpt-4o",
		Error: fmt.Errorf("api error"),
		Metadata: map[string]any{
			"prompt_tokens": 100,
		},
	}
	ct.After(ctx, evt)
	report := ct.GetGlobalCost()
	if report.TotalTokens != 0 {
		t.Errorf("expected 0 tokens on error, got %d", report.TotalTokens)
	}
}

func TestCostTrackerSkipsNonAfterEvents(t *testing.T) {
	ct := NewCostTracker(nil)
	ctx := context.Background()
	evt := &Event{
		Type: EventModelCallBefore,
		Name: "gpt-4o",
		Metadata: map[string]any{
			"prompt_tokens": 500,
		},
	}
	ct.After(ctx, evt)
	if ct.GetGlobalCost().TotalTokens != 0 {
		t.Error("should not accumulate tokens for non-after events")
	}
}

func TestCostTrackerBudgetEnforcement(t *testing.T) {
	ct := NewCostTracker(map[string]ModelPrice{
		"gpt-4o": {PromptPricePerToken: 1.0, CompletionPricePerToken: 2.0},
	})
	ct.Budget = 0.01 // very small budget
	ctx := context.Background()

	// First accumulate cost over budget
	ct.After(ctx, &Event{
		Type: EventModelCallAfter,
		Name: "gpt-4o",
		Metadata: map[string]any{
			"prompt_tokens":     100,
			"completion_tokens": 10,
		},
	})

	// Now Before should fail
	err := ct.Before(ctx, &Event{Type: EventModelCallBefore, Name: "gpt-4o"})
	if err == nil {
		t.Fatal("expected budget exceeded error")
		return
	}
}

func TestCostTrackerBudgetZeroMeansUnlimited(t *testing.T) {
	ct := NewCostTracker(nil)
	ct.Budget = 0
	ctx := context.Background()
	err := ct.Before(ctx, &Event{Type: EventModelCallBefore, Name: "gpt-4o"})
	if err != nil {
		t.Fatalf("unexpected error with zero budget: %v", err)
	}
}

func TestCostTrackerSkipsZeroTokens(t *testing.T) {
	ct := NewCostTracker(nil)
	ctx := context.Background()
	evt := &Event{
		Type:     EventModelCallAfter,
		Name:     "gpt-4o",
		Metadata: map[string]any{},
	}
	ct.After(ctx, evt)
	if ct.GetGlobalCost().TotalTokens != 0 {
		t.Error("should not accumulate zero tokens")
	}
}

func TestCostTrackerDefaultPriceTable(t *testing.T) {
	ct := NewCostTracker(nil)
	if len(ct.priceTable) == 0 {
		t.Error("expected non-empty default price table")
	}
	if _, ok := ct.priceTable["gpt-4o"]; !ok {
		t.Error("expected gpt-4o in default price table")
	}
}

type usageOutput struct {
	prompt     int
	completion int
}

func (u *usageOutput) GetUsage() (prompt, completion int) {
	return u.prompt, u.completion
}

func TestExtractUsage_FromOutputInterface(t *testing.T) {
	ct := NewCostTracker(map[string]ModelPrice{
		"m": {PromptPricePerToken: 0.001, CompletionPricePerToken: 0.002},
	})
	ctx := context.Background()

	evt := &Event{
		Type:   EventModelCallAfter,
		Name:   "m",
		Output: &usageOutput{prompt: 50, completion: 25},
	}
	ct.After(ctx, evt)
	report := ct.GetGlobalCost()
	if report.PromptTokens != 50 {
		t.Errorf("PromptTokens=%d, want 50", report.PromptTokens)
	}
	if report.CompletionTokens != 25 {
		t.Errorf("CompletionTokens=%d, want 25", report.CompletionTokens)
	}
}

func TestCostTracker_BeforeNonModelEvent(t *testing.T) {
	ct := NewCostTracker(nil)
	ct.Budget = 1.0
	err := ct.Before(context.Background(), &Event{Type: EventToolCallBefore, Name: "tool"})
	if err != nil {
		t.Errorf("non-model event should not be checked: %v", err)
	}
}

type costTestProvider struct{ id string }

func (p costTestProvider) Model() string { return p.id }

// Agents emit model call events named after the provider with the request as
// Input and a *model.ChatResponse as Output; the tracker must price the model.
func TestCostTrackerPricesAgentModelCallEvents(t *testing.T) {
	ct := NewCostTracker(map[string]ModelPrice{
		"claude-haiku-4-5": {PromptPricePerToken: 0.000001, CompletionPricePerToken: 0.000005},
	})
	evt := &Event{
		Type:  EventModelCallAfter,
		Name:  "anthropic",
		Input: &model.ChatRequest{Model: "claude-haiku-4-5"},
		Output: &model.ChatResponse{Usage: model.Usage{
			PromptTokens: 100, CacheReadTokens: 900, CompletionTokens: 10,
		}},
	}
	if err := ct.After(context.Background(), evt); err != nil {
		t.Fatalf("After() error = %v", err)
	}
	got := ct.GetGlobalCost()
	// Anthropic-style usage: 100 uncached + 900 cache reads = 1000 input tokens.
	if got.PromptTokens != 1000 || got.CompletionTokens != 10 {
		t.Fatalf("tokens = %+v, want 1000 prompt / 10 completion", got)
	}
	if want := 1000*0.000001 + 10*0.000005; got.TotalCost != want {
		t.Fatalf("TotalCost = %v, want %v", got.TotalCost, want)
	}
	if unknown := ct.UnknownModels(); len(unknown) != 0 {
		t.Fatalf("UnknownModels() = %v, want none", unknown)
	}

	// Without a request, the provider in metadata identifies the model.
	byProvider := &Event{
		Type:     EventModelCallAfter,
		Name:     "anthropic",
		Metadata: map[string]any{"provider": costTestProvider{id: "claude-haiku-4-5"}},
		Output:   &model.ChatResponse{Usage: model.Usage{PromptTokens: 1}},
	}
	if err := ct.After(context.Background(), byProvider); err != nil {
		t.Fatalf("After(provider metadata) error = %v", err)
	}
	if unknown := ct.UnknownModels(); len(unknown) != 0 {
		t.Fatalf("UnknownModels() after provider event = %v, want none", unknown)
	}
}
