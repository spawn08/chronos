package loaders

import (
	"strings"
	"testing"
)

func TestChunkText_ShortInput_Max(t *testing.T) {
	got := chunkText("hi", 100, 0)
	if len(got) != 1 || got[0] != "hi" {
		t.Fatalf("got %v", got)
	}
}

func TestChunkText_OverlapGTESize_StepOne_Max(t *testing.T) {
	// overlap >= size forces step = 1 branch
	s := strings.Repeat("x", 25)
	got := chunkText(s, 5, 10)
	if len(got) < 5 {
		t.Fatalf("expected many small steps, got %d chunks", len(got))
	}
}

func TestChunkText_PositiveOverlap_Max(t *testing.T) {
	s := strings.Repeat("y", 40)
	got := chunkText(s, 12, 4)
	if len(got) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(got))
	}
}
