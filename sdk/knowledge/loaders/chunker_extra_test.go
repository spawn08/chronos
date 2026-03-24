package loaders

import (
	"strings"
	"testing"
)

func TestChunker_mergeUnits_WithOverlap(t *testing.T) {
	c := &Chunker{ChunkSize: 20, Overlap: 5}
	units := []string{
		strings.Repeat("a", 12),
		strings.Repeat("b", 12),
		strings.Repeat("c", 12),
	}
	out := c.mergeUnits(units)
	if len(out) < 2 {
		t.Fatalf("expected multiple chunks, got %d: %#v", len(out), out)
	}
}

func TestChunker_mergeUnits_ZeroChunkSizeNonEmpty(t *testing.T) {
	c := &Chunker{ChunkSize: 0}
	units := []string{"one", "two"}
	out := c.mergeUnits(units)
	if len(out) != 2 {
		t.Errorf("got %#v", out)
	}
}

func TestChunker_mergeUnits_ZeroChunkSizeEmpty(t *testing.T) {
	c := &Chunker{ChunkSize: 0}
	out := c.mergeUnits(nil)
	if out != nil {
		t.Errorf("got %#v, want nil", out)
	}
}
