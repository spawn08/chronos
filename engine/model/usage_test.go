package model

import "testing"

func TestUsageUncachedPromptTokensOpenAIStyle(t *testing.T) {
	u := Usage{PromptTokens: 10400, CacheReadTokens: 10000, CacheReadInPrompt: true}
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

// An Anthropic call with no cache write and more fresh input than cache hits
// used to be misread as OpenAI-style by a count-based heuristic, subtracting
// cache reads from input that never contained them.
func TestUsageAnthropicStyleLargeFreshInputIsNotReduced(t *testing.T) {
	u := Usage{PromptTokens: 5000, CacheReadTokens: 3000}
	if got := u.UncachedPromptTokens(); got != 5000 {
		t.Fatalf("UncachedPromptTokens() = %d, want 5000", got)
	}
	if got := u.PromptWindowTokens(); got != 8000 {
		t.Fatalf("PromptWindowTokens() = %d, want 8000", got)
	}
}

func TestUsageCacheFlagsSurviveMergeAndAdd(t *testing.T) {
	var merged Usage
	merged.Merge(Usage{PromptTokens: 100, CacheReadTokens: 60, CacheReadInPrompt: true})
	merged.Merge(Usage{CompletionTokens: 5})
	if !merged.CacheReadInPrompt || merged.UncachedPromptTokens() != 40 {
		t.Fatalf("Merge lost CacheReadInPrompt: %+v", merged)
	}
	var added Usage
	added.Add(Usage{CacheCreationTokens: 10, CacheCreation1hTokens: 4})
	added.Add(Usage{CacheCreationTokens: 6, CacheCreation1hTokens: 6})
	if added.CacheCreationTokens != 16 || added.CacheCreation1hTokens != 10 {
		t.Fatalf("Add = %+v, want 16 cache writes of which 10 are 1h", added)
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
