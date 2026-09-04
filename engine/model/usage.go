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
	if src.CacheReadTokens > 0 {
		u.CacheReadTokens = src.CacheReadTokens
	}
	if src.ContextTokens > 0 {
		u.ContextTokens = src.ContextTokens
	}
}

// Add accumulates another round's usage into u. Used for multi-round tool loops.
func (u *Usage) Add(src Usage) {
	u.PromptTokens += src.PromptTokens
	u.CompletionTokens += src.CompletionTokens
	u.CacheCreationTokens += src.CacheCreationTokens
	u.CacheReadTokens += src.CacheReadTokens
	if src.ContextTokens > 0 {
		u.ContextTokens = src.ContextTokens
	}
}

// UncachedPromptTokens is the portion of the prompt billed at full input
// price. OpenAI-style providers include cache hits inside PromptTokens;
// Anthropic-style providers report uncached input separately from cache
// reads/writes.
func (u Usage) UncachedPromptTokens() int {
	if u.CacheCreationTokens == 0 && u.CacheReadTokens > 0 && u.PromptTokens >= u.CacheReadTokens {
		return u.PromptTokens - u.CacheReadTokens
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
