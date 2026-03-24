package loaders

import (
	"strings"
	"testing"
)

func TestChunker_Characters(t *testing.T) {
	c := NewChunker(ChunkByCharacters, 10, 0)
	chunks := c.Split("ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	if len(chunks) < 3 {
		t.Errorf("expected at least 3 chunks, got %d", len(chunks))
	}
	if chunks[0] != "ABCDEFGHIJ" {
		t.Errorf("chunk 0 = %q", chunks[0])
	}
}

func TestChunker_Sentences(t *testing.T) {
	text := "First sentence. Second sentence. Third sentence."
	c := NewChunker(ChunkBySentences, 40, 0)
	chunks := c.Split(text)
	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(chunks))
	}
}

func TestChunker_Paragraphs(t *testing.T) {
	text := "Paragraph one.\n\nParagraph two.\n\nParagraph three."
	c := NewChunker(ChunkByParagraphs, 30, 0)
	chunks := c.Split(text)
	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(chunks))
	}
}

func TestChunker_NoLimit(t *testing.T) {
	c := NewChunker(ChunkBySentences, 0, 0)
	chunks := c.Split("One. Two. Three.")
	if len(chunks) != 3 {
		t.Errorf("expected 3 sentences, got %d", len(chunks))
	}
}

func TestSplitSentences(t *testing.T) {
	text := "Hello world. How are you? I'm fine! Thanks."
	sentences := splitSentences(text)
	if len(sentences) != 4 {
		t.Errorf("expected 4 sentences, got %d: %v", len(sentences), sentences)
	}
}

func TestChunker_ShortText(t *testing.T) {
	c := NewChunker(ChunkByCharacters, 1000, 0)
	chunks := c.Split("short")
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
}

func TestChunker_EmptyParagraphs(t *testing.T) {
	c := NewChunker(ChunkByParagraphs, 0, 0)
	chunks := c.Split("\n\n\n\n")
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty paragraphs, got %d", len(chunks))
	}
}

func TestChunker_Overlap(t *testing.T) {
	c := NewChunker(ChunkByCharacters, 10, 3)
	chunks := c.Split(strings.Repeat("A", 25))
	if len(chunks) < 3 {
		t.Errorf("expected at least 3 chunks with overlap, got %d", len(chunks))
	}
}
