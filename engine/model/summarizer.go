package model

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// SummarizationConfig controls when and how auto-summarization triggers.
type SummarizationConfig struct {
	Threshold           float64 // fraction of context window that triggers summarization (default 0.8)
	PreserveRecentTurns int     // number of recent user/assistant pairs to keep verbatim
}

// SummarizationResult holds the output of a summarization pass.
type SummarizationResult struct {
	Summary           string
	PreservedMessages []Message
	SummarizedCount   int
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

	// Never start the preserved tail with tool results: move the split back to
	// the assistant message that requested them so a tool round stays whole.
	split := len(messages) - keepCount
	for split > 0 && messages[split].Role == RoleTool {
		split--
	}
	if split == 0 {
		return SummarizationResult{
			Summary:           existingSummary,
			PreservedMessages: messages,
		}, nil
	}
	toSummarize := messages[:split]
	toKeep := messages[split:]

	var b strings.Builder
	if existingSummary != "" {
		b.WriteString("Previous summary:\n")
		b.WriteString(existingSummary)
		b.WriteString("\n\nNew messages to incorporate:\n")
	} else {
		b.WriteString("Summarize the following conversation concisely, preserving key facts, decisions, and context:\n\n")
	}
	for i := range toSummarize {
		writeSummaryLine(&b, toSummarize[i])
	}

	resp, err := s.provider.Chat(ctx, &ChatRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: "You are a conversation summarizer. Create a concise summary that preserves all important context, decisions, and facts, including tool actions taken, their outcomes, and work that remains."},
			{Role: RoleUser, Content: b.String()},
		},
		MaxTokens: summaryMaxTokens,
		Model:     s.provider.Model(),
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

const (
	// summaryMaxTokens bounds the summary output. A summary may stand in for
	// many tool rounds of a long task, so it needs more room than a chat recap.
	summaryMaxTokens = 1500
	// summaryToolTextLimit bounds each tool argument/result rendered into the
	// summarization prompt so the prompt itself stays within the window.
	summaryToolTextLimit = 1000
)

// writeSummaryLine renders one message for the summarization prompt. Tool
// calls and bounded tool results are included: in agentic turns they carry
// most of the facts (files changed, commands run, failures seen).
func writeSummaryLine(b *strings.Builder, msg Message) {
	switch {
	case msg.Role == RoleTool:
		fmt.Fprintf(b, "tool result (%s): %s\n", msg.Name, truncateSummaryText(msg.Content))
	case len(msg.ToolCalls) > 0:
		if msg.Content != "" {
			fmt.Fprintf(b, "%s: %s\n", msg.Role, msg.Content)
		}
		for _, call := range msg.ToolCalls {
			fmt.Fprintf(b, "%s called %s(%s)\n", msg.Role, call.Name, truncateSummaryText(call.Arguments))
		}
	default:
		fmt.Fprintf(b, "%s: %s\n", msg.Role, msg.Content)
	}
}

func truncateSummaryText(text string) string {
	if len(text) <= summaryToolTextLimit {
		return text
	}
	cut := summaryToolTextLimit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…[truncated]"
}

// EstimateTokens provides a rough token estimate for a list of messages.
func EstimateTokens(messages []Message) int {
	c := NewEstimatingCounter()
	return c.CountTokens(messages)
}
