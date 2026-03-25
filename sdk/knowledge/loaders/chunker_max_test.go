package loaders

import (
	"strings"
	"testing"
)

func TestSplitSentences_MaxBranches(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"Hello! World.", 2},
		{"Really?", 1},
		{"No end punctuation", 1},
		{"A. B! C?", 3},
		{"End. Next", 2},
		{"Dot without space after.No split here", 1},
	}
	for _, tc := range cases {
		got := splitSentences(tc.in)
		if len(got) != tc.want {
			t.Errorf("splitSentences(%q) = %d parts %v, want %d", tc.in, len(got), got, tc.want)
		}
	}
}

func TestChunker_OverlapTailMax(t *testing.T) {
	c := NewChunker(ChunkByCharacters, 20, 15)
	// Long text so overlap branch in splitWords runs (tail longer than overlap)
	text := strings.Repeat("word ", 30)
	chunks := c.Split(text)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
}
