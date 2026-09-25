package model

// Merge overlays src onto u. Non-zero prompt/cache/context fields replace
// the previous value (providers emit them once per call). Completion tokens
// take the max so streamed output counts never regress.
func (u *Usage) Merge(src Usage) {
	if src.PromptTokens > 0 {
		u.PromptTokens = src.PromptTokens
	}
	if src.CompletionTokens > u.CompletionTokens {
		u.CompletionTokens = src.CompletionTokens
	}
	if src.CacheCreationTokens > 0 {
		u.CacheCreationTokens = src.CacheCreationTokens
	}
	if src.CacheCreation1hTokens > 0 {
		u.CacheCreation1hTokens = src.CacheCreation1hTokens
	}
	if src.CacheReadTokens > 0 {
		u.CacheReadTokens = src.CacheReadTokens
	}
	if src.ContextTokens > 0 {
		u.ContextTokens = src.ContextTokens
	}
	u.CacheReadInPrompt = u.CacheReadInPrompt || src.CacheReadInPrompt
}

// Add accumulates another round's usage into u. Used for multi-round tool
// loops, whose rounds share one provider and therefore one cache convention.
func (u *Usage) Add(src Usage) {
	u.PromptTokens += src.PromptTokens
	u.CompletionTokens += src.CompletionTokens
	u.CacheCreationTokens += src.CacheCreationTokens
	u.CacheCreation1hTokens += src.CacheCreation1hTokens
	u.CacheReadTokens += src.CacheReadTokens
	if src.ContextTokens > 0 {
		u.ContextTokens = src.ContextTokens
	}
	u.CacheReadInPrompt = u.CacheReadInPrompt || src.CacheReadInPrompt
}

// UncachedPromptTokens is the portion of the prompt billed at full input
// price. When the provider declares CacheReadInPrompt, cache hits are removed
// from PromptTokens; otherwise PromptTokens is already uncached input.
func (u Usage) UncachedPromptTokens() int {
	if u.CacheReadInPrompt {
		return max(u.PromptTokens-u.CacheReadTokens, 0)
	}
	return u.PromptTokens
}

// PromptWindowTokens is the prompt-side context occupied by this call:
// uncached input plus cache writes and cache hits.
func (u Usage) PromptWindowTokens() int {
	return u.UncachedPromptTokens() + u.CacheCreationTokens + u.CacheReadTokens
}

// WindowTokens is the full context occupied by this call when ContextTokens
// is unset: prompt window plus completion.
func (u Usage) WindowTokens() int {
	if u.ContextTokens > 0 {
		return u.ContextTokens
	}
	return u.PromptWindowTokens() + u.CompletionTokens
}
