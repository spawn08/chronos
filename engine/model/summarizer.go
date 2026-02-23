package model

import (
	"context"
	"fmt"
	"strings"
)

const defaultSummarizationPrompt = `You are a conversation summarizer. Given a conversation (and optionally a prior summary), produce a concise summary that preserves:
- Key facts and decisions made
- Tool call results and their outcomes
- User preferences and requirements
- Any unresolved questions or pending actions

Be concise but thorough. Do not lose important details. Output only the summary text, no preamble.`

// SummarizationConfig controls when and how conversation summarization occurs.
type SummarizationConfig struct {
	// Threshold is the fraction of the context window that triggers summarization (default 0.8).
	Threshold float64
	// PreserveRecentTurns is the number of recent user/assistant message pairs to keep
	// unsummarized so the model retains immediate conversational context (default 5).
	PreserveRecentTurns int
	// MaxSummaryTokens caps the length of the generated summary (default 1024).
	MaxSummaryTokens int
	// Prompt overrides the default summarization system prompt.
	Prompt string
}

func (c *SummarizationConfig) threshold() float64 {
	if c.Threshold > 0 && c.Threshold < 1 {
		return c.Threshold
	}
	return 0.8
}

func (c *SummarizationConfig) preserveTurns() int {
	if c.PreserveRecentTurns > 0 {
		return c.PreserveRecentTurns
	}
	return 5
}

func (c *SummarizationConfig) maxSummaryTokens() int {
	if c.MaxSummaryTokens > 0 {
		return c.MaxSummaryTokens
	}
	return 1024
}

func (c *SummarizationConfig) prompt() string {
	if c.Prompt != "" {
		return c.Prompt
	}
	return defaultSummarizationPrompt
}

// Summarizer produces rolling summaries of conversation history using an LLM.
type Summarizer struct {
	provider Provider
	counter  TokenCounter
	config   SummarizationConfig
}

// NewSummarizer creates a summarizer backed by the given provider and token counter.
func NewSummarizer(provider Provider, counter TokenCounter, cfg SummarizationConfig) *Summarizer {
	if counter == nil {
		counter = NewEstimatingCounter()
	}
	return &Summarizer{
		provider: provider,
		counter:  counter,
		config:   cfg,
	}
}

// SummarizeResult holds the output of a summarization pass.
type SummarizeResult struct {
	Summary          string    // the new rolling summary
	PreservedMessages []Message // recent messages kept intact
}

// NeedsSummarization returns true when the estimated token count of the messages
// (plus system context) exceeds the configured threshold of the context window.
func (s *Summarizer) NeedsSummarization(systemTokens int, messages []Message, contextLimit int) bool {
	msgTokens := s.counter.CountTokens(messages)
	total := systemTokens + msgTokens
	threshold := int(float64(contextLimit) * s.config.threshold())
	return total > threshold
}

// Summarize compresses older messages into a rolling summary, preserving the
// most recent turns. If priorSummary is non-empty it is incorporated into the
// new summary so no context is lost across multiple summarization passes.
func (s *Summarizer) Summarize(ctx context.Context, priorSummary string, messages []Message) (*SummarizeResult, error) {
	preserve := s.config.preserveTurns()

	// Count message pairs (user + assistant = 1 turn). Walk backwards to find
	// the split point that keeps at least `preserve` turns of recent messages.
	splitIdx := findSplitIndex(messages, preserve)

	toSummarize := messages[:splitIdx]
	preserved := messages[splitIdx:]

	if len(toSummarize) == 0 {
		return &SummarizeResult{Summary: priorSummary, PreservedMessages: preserved}, nil
	}

	var convo strings.Builder
	if priorSummary != "" {
		convo.WriteString("Prior conversation summary:\n")
		convo.WriteString(priorSummary)
		convo.WriteString("\n\n---\nNew messages to incorporate:\n")
	}
	for _, m := range toSummarize {
		convo.WriteString(m.Role)
		convo.WriteString(": ")
		convo.WriteString(m.Content)
		if len(m.ToolCalls) > 0 {
			convo.WriteString(" [tool calls: ")
			names := make([]string, len(m.ToolCalls))
			for i, tc := range m.ToolCalls {
				names[i] = tc.Name
			}
			convo.WriteString(strings.Join(names, ", "))
			convo.WriteString("]")
		}
		convo.WriteString("\n")
	}

	resp, err := s.provider.Chat(ctx, &ChatRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: s.config.prompt()},
			{Role: RoleUser, Content: convo.String()},
		},
		MaxTokens:   s.config.maxSummaryTokens(),
		Temperature: 0.0,
	})
	if err != nil {
		return nil, fmt.Errorf("summarizer: %w", err)
	}

	return &SummarizeResult{
		Summary:           resp.Content,
		PreservedMessages: preserved,
	}, nil
}

// findSplitIndex walks messages from the end and counts user/assistant turn
// pairs. It returns the index that separates "old" messages (to summarize)
// from "recent" messages (to preserve).
func findSplitIndex(messages []Message, preserveTurns int) int {
	if preserveTurns <= 0 || len(messages) == 0 {
		return 0
	}
	turns := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == RoleUser {
			turns++
		}
		if turns >= preserveTurns {
			return i
		}
	}
	return 0
}
