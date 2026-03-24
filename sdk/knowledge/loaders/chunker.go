package loaders

import (
	"strings"
	"unicode"
)

// ChunkStrategy defines how text is split into chunks.
type ChunkStrategy int

const (
	ChunkByCharacters ChunkStrategy = iota
	ChunkBySentences
	ChunkByParagraphs
)

// Chunker splits text into chunks using configurable strategies.
type Chunker struct {
	Strategy  ChunkStrategy
	ChunkSize int
	Overlap   int
}

// NewChunker creates a chunker with the given strategy.
func NewChunker(strategy ChunkStrategy, chunkSize, overlap int) *Chunker {
	return &Chunker{Strategy: strategy, ChunkSize: chunkSize, Overlap: overlap}
}

// Split divides text into chunks according to the configured strategy.
func (c *Chunker) Split(text string) []string {
	switch c.Strategy {
	case ChunkBySentences:
		return c.chunkBySentences(text)
	case ChunkByParagraphs:
		return c.chunkByParagraphs(text)
	default:
		return chunkText(text, c.ChunkSize, c.Overlap)
	}
}

func (c *Chunker) chunkBySentences(text string) []string {
	sentences := splitSentences(text)
	return c.mergeUnits(sentences)
}

func (c *Chunker) chunkByParagraphs(text string) []string {
	paragraphs := strings.Split(text, "\n\n")
	var cleaned []string
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	return c.mergeUnits(cleaned)
}

func (c *Chunker) mergeUnits(units []string) []string {
	if c.ChunkSize <= 0 {
		if len(units) == 0 {
			return nil
		}
		return units
	}

	var chunks []string
	var current strings.Builder
	for _, u := range units {
		if current.Len() > 0 && current.Len()+len(u)+1 > c.ChunkSize {
			chunks = append(chunks, strings.TrimSpace(current.String()))
			if c.Overlap > 0 && current.Len() > c.Overlap {
				tail := current.String()
				current.Reset()
				if len(tail) > c.Overlap {
					current.WriteString(tail[len(tail)-c.Overlap:])
				}
			} else {
				current.Reset()
			}
		}
		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(u)
	}
	if current.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(current.String()))
	}
	return chunks
}

func splitSentences(text string) []string {
	var sentences []string
	var current strings.Builder
	runes := []rune(text)
	for i, r := range runes {
		current.WriteRune(r)
		if (r == '.' || r == '!' || r == '?') && (i+1 >= len(runes) || unicode.IsSpace(runes[i+1])) {
			s := strings.TrimSpace(current.String())
			if s != "" {
				sentences = append(sentences, s)
			}
			current.Reset()
		}
	}
	if s := strings.TrimSpace(current.String()); s != "" {
		sentences = append(sentences, s)
	}
	return sentences
}
