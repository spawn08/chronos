package model

import "testing"

func TestUsageUncachedPromptTokensOpenAIStyle(t *testing.T) {
	u := Usage{PromptTokens: 10400, CacheReadTokens: 10000}
	if got := u.UncachedPromptTokens(); got != 400 {
		t.Fatalf("UncachedPromptTokens() = %d, want 400", got)
	}
	if got := u.PromptWindowTokens(); got != 10400 {
		t.Fatalf("PromptWindowTokens() = %d, want 10400", got)
	}
}

func TestUsageUncachedPromptTokensAnthropicStyle(t *testing.T) {
	u := Usage{PromptTokens: 400, CacheReadTokens: 10000, CacheCreationTokens: 80}
	if got := u.UncachedPromptTokens(); got != 400 {
		t.Fatalf("UncachedPromptTokens() = %d, want 400", got)
	}
	if got := u.PromptWindowTokens(); got != 10480 {
		t.Fatalf("PromptWindowTokens() = %d, want 10480", got)
	}
}

func TestUsageMergeAndAdd(t *testing.T) {
	var u Usage
	u.Merge(Usage{PromptTokens: 10, CacheReadTokens: 100})
	u.Merge(Usage{CompletionTokens: 4})
	if u.PromptTokens != 10 || u.CacheReadTokens != 100 || u.CompletionTokens != 4 {
		t.Fatalf("Merge = %+v", u)
	}
	u.Add(Usage{PromptTokens: 2, CompletionTokens: 1, CacheCreationTokens: 5})
	if u.PromptTokens != 12 || u.CompletionTokens != 5 || u.CacheCreationTokens != 5 {
		t.Fatalf("Add = %+v", u)
	}
}
