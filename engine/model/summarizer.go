package model

import (
	"context"
	"fmt"
	"strings"
)

// SummarizationConfig controls when and how auto-summarization triggers.
type SummarizationConfig struct {
	Threshold           float64 // fraction of context window that triggers summarization (default 0.8)
	PreserveRecentTurns int     // number of recent user/assistant pairs to keep verbatim
}

// SummarizationResult holds the output of a summarization pass.
type SummarizationResult struct {
	Summary            string
	PreservedMessages  []Message
	SummarizedCount    int
}

// Summarizer compresses conversation history to fit within context windows.
type Summarizer struct {
	provider Provider
	counter  TokenCounter
	config   SummarizationConfig
}

// NewSummarizer creates a summarizer with the given provider, counter, and config.
func NewSummarizer(p Provider, counter TokenCounter, cfg SummarizationConfig) *Summarizer {
	if cfg.Threshold <= 0 {
		cfg.Threshold = 0.8
	}
	if cfg.PreserveRecentTurns <= 0 {
		cfg.PreserveRecentTurns = 5
	}
	return &Summarizer{provider: p, counter: counter, config: cfg}
}

// NeedsSummarization returns true if the total estimated tokens (system + history)
// exceed the threshold fraction of the context limit.
func (s *Summarizer) NeedsSummarization(systemTokens int, history []Message, contextLimit int) bool {
	historyTokens := s.counter.CountTokens(history)
	total := systemTokens + historyTokens
	return total > int(float64(contextLimit)*s.config.Threshold)
}

// Summarize compresses older messages into a running summary. If an existing
// summary is provided, it is incorporated into the new summary.
func (s *Summarizer) Summarize(ctx context.Context, existingSummary string, messages []Message) (SummarizationResult, error) {
	if len(messages) == 0 {
		return SummarizationResult{Summary: existingSummary, PreservedMessages: messages}, nil
	}

	keepCount := s.config.PreserveRecentTurns * 2
	if keepCount >= len(messages) {
		return SummarizationResult{
			Summary:           existingSummary,
			PreservedMessages: messages,
		}, nil
	}

	toSummarize := messages[:len(messages)-keepCount]
	toKeep := messages[len(messages)-keepCount:]

	var b strings.Builder
	if existingSummary != "" {
		b.WriteString("Previous summary:\n")
		b.WriteString(existingSummary)
		b.WriteString("\n\nNew messages to incorporate:\n")
	} else {
		b.WriteString("Summarize the following conversation concisely, preserving key facts, decisions, and context:\n\n")
	}
	for _, m := range toSummarize {
		fmt.Fprintf(&b, "%s: %s\n", m.Role, m.Content)
	}

	resp, err := s.provider.Chat(ctx, &ChatRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: "You are a conversation summarizer. Create a concise summary that preserves all important context, decisions, and facts."},
			{Role: RoleUser, Content: b.String()},
		},
		MaxTokens: 500,
	})
	if err != nil {
		return SummarizationResult{}, fmt.Errorf("summarize: %w", err)
	}

	return SummarizationResult{
		Summary:           resp.Content,
		PreservedMessages: toKeep,
		SummarizedCount:   len(toSummarize),
	}, nil
}

// EstimateTokens provides a rough token estimate for a list of messages.
func EstimateTokens(messages []Message) int {
	c := NewEstimatingCounter()
	return c.CountTokens(messages)
}
