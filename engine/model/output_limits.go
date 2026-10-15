package model

import "strings"

// AnthropicOutputTokens returns an output allowance every listed Claude model
// family accepts, or 0 for a non-Claude model or one limited to the
// provider's 4096-token default. Claude 3.7 and later accept at least 32000
// output tokens; 3.5 accepts 8192.
func AnthropicOutputTokens(modelName string) int {
	id := platformModelID(modelName)
	switch {
	case !strings.Contains(id, "claude"):
		return 0
	case strings.Contains(id, "claude-3-5") || strings.Contains(id, "claude-3.5"):
		return 8192
	case strings.Contains(id, "claude-3-7") || strings.Contains(id, "claude-3.7"):
		return 32000
	case strings.Contains(id, "claude-3") || strings.Contains(id, "claude-2") || strings.Contains(id, "claude-instant"):
		return 0
	}
	return 32000
}

// KnownOutputLimit returns the per-response output limit for Claude families
// whose limit is below the summary budget's maximum, or 0 when the model is
// unknown or accepts at least summaryMaxTokens. It shares the family table
// of AnthropicOutputTokens; an allowance of 0 there for a Claude model means
// the 4096-token provider default.
func KnownOutputLimit(modelName string) int {
	if !strings.Contains(platformModelID(modelName), "claude") {
		return 0
	}
	limit := AnthropicOutputTokens(modelName)
	switch {
	case limit == 0:
		return 4096
	case limit < summaryMaxTokens:
		return limit
	}
	return 0
}
