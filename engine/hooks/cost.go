package hooks

import (
	"context"
	"fmt"
	"sync"
)

// ModelPrice defines the per-token cost for a model.
type ModelPrice struct {
	PromptPricePerToken     float64 // cost per prompt token (e.g. $0.000003)
	CompletionPricePerToken float64 // cost per completion token
}

// CostReport is a snapshot of accumulated costs.
type CostReport struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	TotalCost        float64 `json:"total_cost"`
	Currency         string  `json:"currency"`
}

// CostTracker is a hook that tracks LLM API costs per session and enforces
// optional budget limits. It intercepts model call events to accumulate token
// usage and compute costs using a configurable price table.
type CostTracker struct {
	mu         sync.Mutex
	sessions   map[string]*CostReport
	global     CostReport
	priceTable map[string]ModelPrice
	// Budget is the maximum total spend (in currency units) before calls are
	// blocked. 0 means unlimited.
	Budget float64
}

// NewCostTracker creates a cost tracker with the given price table.
// The priceTable maps model names (e.g. "gpt-4o") to their per-token prices.
func NewCostTracker(priceTable map[string]ModelPrice) *CostTracker {
	if priceTable == nil {
		priceTable = defaultPriceTable()
	}
	return &CostTracker{
		sessions:   make(map[string]*CostReport),
		priceTable: priceTable,
		global:     CostReport{Currency: "USD"},
	}
}

func (ct *CostTracker) Before(_ context.Context, evt *Event) error {
	if evt.Type != EventModelCallBefore {
		return nil
	}
	if ct.Budget <= 0 {
		return nil
	}
	ct.mu.Lock()
	defer ct.mu.Unlock()
	if ct.global.TotalCost >= ct.Budget {
		return fmt.Errorf("cost budget exceeded: spent $%.4f of $%.4f budget", ct.global.TotalCost, ct.Budget)
	}
	return nil
}

func (ct *CostTracker) After(_ context.Context, evt *Event) error {
	if evt.Type != EventModelCallAfter {
		return nil
	}
	if evt.Error != nil {
		return nil
	}

	modelName := evt.Name
	promptTokens, completionTokens := extractUsage(evt)
	if promptTokens == 0 && completionTokens == 0 {
		return nil
	}

	price := ct.priceTable[modelName]
	cost := float64(promptTokens)*price.PromptPricePerToken +
		float64(completionTokens)*price.CompletionPricePerToken

	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.global.PromptTokens += promptTokens
	ct.global.CompletionTokens += completionTokens
	ct.global.TotalTokens += promptTokens + completionTokens
	ct.global.TotalCost += cost

	sessionID := ""
	if evt.Metadata != nil {
		sessionID, _ = evt.Metadata["session_id"].(string)
	}
	if sessionID != "" {
		sr, ok := ct.sessions[sessionID]
		if !ok {
			sr = &CostReport{Currency: "USD"}
			ct.sessions[sessionID] = sr
		}
		sr.PromptTokens += promptTokens
		sr.CompletionTokens += completionTokens
		sr.TotalTokens += promptTokens + completionTokens
		sr.TotalCost += cost
	}
	return nil
}

// GetGlobalCost returns the accumulated cost across all sessions.
func (ct *CostTracker) GetGlobalCost() CostReport {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.global
}

// GetSessionCost returns the accumulated cost for a specific session.
func (ct *CostTracker) GetSessionCost(sessionID string) CostReport {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	if sr, ok := ct.sessions[sessionID]; ok {
		return *sr
	}
	return CostReport{Currency: "USD"}
}

// extractUsage pulls token counts from a model call after event. It looks at
// the Output field (expected to be a *ChatResponse or compatible struct with
// Usage) and also checks metadata.
func extractUsage(evt *Event) (prompt, completion int) {
	type usageHolder struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}

	if evt.Metadata != nil {
		if p, ok := evt.Metadata["prompt_tokens"].(int); ok {
			prompt = p
		}
		if c, ok := evt.Metadata["completion_tokens"].(int); ok {
			completion = c
		}
		if prompt > 0 || completion > 0 {
			return
		}
	}

	// Try to extract from output via interface
	type usageGetter interface {
		GetUsage() (int, int)
	}
	if ug, ok := evt.Output.(usageGetter); ok {
		prompt, completion = ug.GetUsage()
		return
	}

	return 0, 0
}

func defaultPriceTable() map[string]ModelPrice {
	return map[string]ModelPrice{
		"gpt-4o":      {PromptPricePerToken: 0.0000025, CompletionPricePerToken: 0.00001},
		"gpt-4o-mini": {PromptPricePerToken: 0.00000015, CompletionPricePerToken: 0.0000006},
		"gpt-4-turbo": {PromptPricePerToken: 0.00001, CompletionPricePerToken: 0.00003},
		"o1":          {PromptPricePerToken: 0.000015, CompletionPricePerToken: 0.00006},
		"o1-mini":     {PromptPricePerToken: 0.000003, CompletionPricePerToken: 0.000012},
		"o3":          {PromptPricePerToken: 0.00001, CompletionPricePerToken: 0.00004},
		"o3-mini":     {PromptPricePerToken: 0.0000011, CompletionPricePerToken: 0.0000044},

		"claude-sonnet-4-6": {PromptPricePerToken: 0.000003, CompletionPricePerToken: 0.000015},
		"claude-3-5-sonnet": {PromptPricePerToken: 0.000003, CompletionPricePerToken: 0.000015},
		"claude-3-opus":     {PromptPricePerToken: 0.000015, CompletionPricePerToken: 0.000075},
		"claude-3-haiku":    {PromptPricePerToken: 0.00000025, CompletionPricePerToken: 0.00000125},
		"claude-3-5-haiku":  {PromptPricePerToken: 0.0000008, CompletionPricePerToken: 0.000004},

		"gemini-2.0-flash": {PromptPricePerToken: 0.00000015, CompletionPricePerToken: 0.0000006},
		"gemini-1.5-pro":   {PromptPricePerToken: 0.00000125, CompletionPricePerToken: 0.000005},

		"mistral-large-latest": {PromptPricePerToken: 0.000002, CompletionPricePerToken: 0.000006},
	}
}
