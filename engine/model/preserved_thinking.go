package model

import (
	"strconv"
	"strings"
)

// AnthropicThinkingBoundToConversation reports whether modelID runs
// Anthropic's preserved-thinking conversation check: a replayed thinking
// block is valid only while every message before it is unchanged. Editing,
// trimming, or compacting earlier turns invalidates every later block, and
// enforced accounts reject such a request. Claude Fable 5.1 and the 5.5
// Opus, Sonnet, and Haiku models (and later) run it; Mythos does not.
func AnthropicThinkingBoundToConversation(modelID string) bool {
	m := anthropicModelVersion.FindStringSubmatch(strings.ToLower(modelID))
	if m == nil {
		return false
	}
	major, _ := strconv.Atoi(m[2])
	minor, _ := strconv.Atoi(m[3])
	switch m[1] {
	case "mythos":
		return false
	case "fable":
		return major > 5 || (major == 5 && minor >= 1)
	}
	return major > 5 || (major == 5 && minor >= 5)
}

// StripAnthropicThinking returns messages with Anthropic thinking state
// removed, leaving text, tool calls, and other providers' state intact. Call
// it once on the history kept after an edit of earlier turns: blocks created
// against the old history no longer verify, and blocks created after the
// edit then bind to the new, append-only history.
func StripAnthropicThinking(messages []Message) []Message {
	var out []Message
	for i := range messages {
		if len(anthropicThinkingBlocks(messages[i].ProviderState)) == 0 {
			continue
		}
		if out == nil {
			out = append([]Message(nil), messages...)
		}
		out[i].ProviderState = nil
	}
	if out == nil {
		return messages
	}
	return out
}
