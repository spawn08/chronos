package model

import "testing"

func TestNewTokenCounter_ReturnsBPEForKnownModel(t *testing.T) {
	c := NewTokenCounter("gpt-4o")
	if _, ok := c.(*BPECounter); !ok {
		t.Fatalf("expected *BPECounter for gpt-4o, got %T", c)
	}
}

func TestNewTokenCounter_UnknownModelFallsBackToBPE(t *testing.T) {
	// Anthropic models are not in tiktoken's model map, but NewBPECounter falls
	// back to a general-purpose encoding rather than the char heuristic.
	c := NewTokenCounter("claude-opus-4-8")
	if _, ok := c.(*BPECounter); !ok {
		t.Fatalf("expected *BPECounter fallback, got %T", c)
	}
}

func TestBPECounter_CountString(t *testing.T) {
	c, err := NewBPECounter("gpt-4o")
	if err != nil {
		t.Fatalf("NewBPECounter: %v", err)
	}
	tests := []struct {
		name string
		in   string
		want int // exact o200k_base counts
	}{
		{"empty", "", 0},
		{"hello", "hello", 1},
		{"sentence", "Hello world, this is a tokenizer test.", 9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.CountString(tt.in); got != tt.want {
				t.Errorf("CountString(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestBPECounter_MoreAccurateThanHeuristic(t *testing.T) {
	// 12 space-separated words tokenize to 12 BPE tokens (each word becomes one
	// token). The len/4 char heuristic yields a materially different (less
	// accurate) number, demonstrating the BPE counter is not the old estimate.
	text := "the quick brown fox jumps over the lazy dog again and again"
	bpe, err := NewBPECounter("gpt-4o")
	if err != nil {
		t.Fatalf("NewBPECounter: %v", err)
	}
	est := NewEstimatingCounter()
	bpeCount := bpe.CountString(text)
	if bpeCount != 12 {
		t.Errorf("BPE count = %d, want exact 12 for 12 words", bpeCount)
	}
	if bpeCount == est.CountString(text) {
		t.Errorf("BPE (%d) should differ from the char heuristic (%d)", bpeCount, est.CountString(text))
	}
}

func TestBPECounter_CountTokens(t *testing.T) {
	c, err := NewBPECounter("gpt-4o")
	if err != nil {
		t.Fatalf("NewBPECounter: %v", err)
	}
	msgs := []Message{
		{Role: RoleSystem, Content: "You are helpful."},
		{Role: RoleUser, Content: "Hello world."},
	}
	got := c.CountTokens(msgs)
	// Two messages: 2*perMessageOverhead + conversationOverhead + content tokens.
	minExpected := 2*perMessageOverhead + conversationOverhead
	if got <= minExpected {
		t.Errorf("CountTokens = %d, want > %d (framing + content)", got, minExpected)
	}
}
