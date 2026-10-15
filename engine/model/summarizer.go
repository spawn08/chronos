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
	// MaxPreservedTokens caps the verbatim tail; 0 keeps PreserveRecentTurns
	// whatever its size. A tail that alone exceeds the trigger would make
	// every later round compact again, rewriting the cached prompt each time.
	MaxPreservedTokens int
	// ContextLimit, when set, caps the summary budget at a share of the
	// window, so a summary of a small-window model still frees space and its
	// request (prompt plus continuations) fits. 0 leaves it uncapped.
	ContextLimit int
	// MaxOutputTokens, when set, caps one summary request's output at the
	// model's limit. 0 leaves the budget's own bounds.
	MaxOutputTokens int
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

// TriggerTokens is the prompt size (system + history) above which
// NeedsSummarization reports true for contextLimit.
func (s *Summarizer) TriggerTokens(contextLimit int) int {
	return int(float64(contextLimit) * s.config.Threshold)
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
	// Deep cut: drop the oldest kept messages, whole tool rounds at a time,
	// until the tail fits its budget. The newest round always stays.
	if limit := s.config.MaxPreservedTokens; limit > 0 {
		for split < len(messages)-1 && s.counter.CountTokens(messages[split:]) > limit {
			next := split + 1
			for next < len(messages) && messages[next].Role == RoleTool {
				next++
			}
			if next >= len(messages) {
				break
			}
			split = next
		}
	}
	toSummarize := messages[:split]
	toKeep := messages[split:]

	var b strings.Builder
	if existingSummary != "" {
		b.WriteString("Previous summary:\n")
		b.WriteString(existingSummary)
		b.WriteString("\n\nNew messages to incorporate:\n")
	} else {
		b.WriteString("Summarize the following conversation for the agent that will continue it:\n\n")
	}
	for i := range toSummarize {
		writeSummaryLine(&b, toSummarize[i])
	}

	summary, err := s.summarize(ctx, b.String(), s.budget(s.counter.CountTokens(toSummarize)))
	if err != nil {
		return SummarizationResult{}, err
	}

	if AnthropicThinkingBoundToConversation(s.provider.Model()) {
		toKeep = StripAnthropicThinking(toKeep)
	}
	return SummarizationResult{
		Summary:           summary,
		PreservedMessages: toKeep,
		SummarizedCount:   len(toSummarize),
	}, nil
}

// summarySystemPrompt asks for the state an agent needs to continue the task.
// The sections keep what a free-form summary most often lost: files already
// understood, approaches already ruled out, and results of checks already run.
const summarySystemPrompt = `You are a conversation summarizer. You compress an AI coding agent's working history so it can continue the task without redoing work. Write these sections, omitting any that would be empty:

## Goal
The user's request and acceptance criteria, in the user's words where possible.
## Decisions
Decisions made and why.
## Files
Each file read or changed: path, what it contains or what changed, and why it matters. Include key line numbers and symbols.
## Commands and checks
Commands run that matter, with their outcome (exit code, pass/fail, key numbers).
## Findings
Facts established, including results returned by subagents, with file:line where known.
## Failed approaches
What was tried and did not work, and why.
## Open items
Remaining work and the next step.

Be specific: keep paths, identifiers, numbers and error messages exactly. Do not invent anything.`

const (
	// summaryMinTokens and summaryMaxTokens bound the summary output, which
	// scales with the history it replaces (summaryBudget). A summary may
	// stand in for many tool rounds of a long task.
	summaryMinTokens = 4096
	summaryMaxTokens = 16384
	// summaryContinuations bounds continuation requests when a summary stops
	// at its output limit; a truncated summary loses the newest work.
	summaryContinuations = 2
	// summaryToolTextLimit bounds each tool argument/result rendered into the
	// summarization prompt so the prompt itself stays within the window.
	summaryToolTextLimit = 1000
)

// writeSummaryLine renders one message for the summarization prompt. Tool
// calls and bounded tool results are included: in agentic turns they carry
// most of the facts (files changed, commands run, failures seen).
// summaryBudget is about a tenth of the summarized history, within
// [summaryMinTokens, summaryMaxTokens].
func summaryBudget(historyTokens int) int {
	return min(max(historyTokens/10, summaryMinTokens), summaryMaxTokens)
}

// summaryContextDivisor caps one summary request's output at 1/20 of the
// context window, so even with every continuation the summary stays a small
// share of the window it is meant to free.
const summaryContextDivisor = 20

// summaryFloorTokens is the smallest budget ever requested.
const summaryFloorTokens = 256

// budget applies the configured window and output caps to summaryBudget.
func (s *Summarizer) budget(historyTokens int) int {
	b := summaryBudget(historyTokens)
	if limit := s.config.ContextLimit; limit > 0 {
		b = min(b, limit/summaryContextDivisor)
	}
	if limit := s.config.MaxOutputTokens; limit > 0 {
		b = min(b, limit)
	}
	return max(b, summaryFloorTokens)
}

// summarize requests a summary and continues it while the model stops at its
// output limit, so the newest history is never cut off the end.
func (s *Summarizer) summarize(ctx context.Context, prompt string, maxTokens int) (string, error) {
	messages := []Message{
		{Role: RoleSystem, Content: summarySystemPrompt},
		{Role: RoleUser, Content: prompt},
	}
	var summary strings.Builder
	for attempt := 0; ; attempt++ {
		resp, err := s.provider.Chat(ctx, &ChatRequest{Messages: messages, MaxTokens: maxTokens, Model: s.provider.Model()})
		if err != nil {
			return "", fmt.Errorf("summarize: %w", err)
		}
		summary.WriteString(resp.Content)
		if resp.StopReason != StopReasonMaxTokens || attempt >= summaryContinuations || resp.Content == "" {
			if strings.TrimSpace(summary.String()) == "" {
				// An empty summary would replace the history with nothing.
				return "", fmt.Errorf("summarize: model returned an empty summary (stop reason %q)", resp.StopReason)
			}
			return summary.String(), nil
		}
		messages = append(messages,
			Message{Role: RoleAssistant, Content: resp.Content},
			Message{Role: RoleUser, Content: "Continue the summary exactly where it stopped. Do not repeat earlier text."})
	}
}

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

// KnownOutputLimit returns the per-response output limit for model families
// whose limit is below the summary budget's maximum, or 0 when the model is
// unknown or accepts at least summaryMaxTokens. Current Claude, GPT and Gemini
// models accept far more than a summary needs.
func KnownOutputLimit(modelName string) int {
	id := platformModelID(modelName)
	switch {
	case strings.Contains(id, "claude-3-5") || strings.Contains(id, "claude-3.5"):
		return 8192
	case strings.Contains(id, "claude-3-7") || strings.Contains(id, "claude-3.7"):
		return 0
	case strings.Contains(id, "claude-3") || strings.Contains(id, "claude-2") || strings.Contains(id, "claude-instant"):
		return 4096
	}
	return 0
}
