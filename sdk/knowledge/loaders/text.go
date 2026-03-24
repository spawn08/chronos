// Package loaders provides document loaders for different file formats.
package loaders

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spawn08/chronos/sdk/knowledge"
)

// TextLoader loads plain text files as knowledge documents.
type TextLoader struct {
	paths     []string
	chunkSize int
	overlap   int
}

// NewTextLoader creates a loader for plain text files.
// chunkSize controls the maximum characters per document chunk (0 = no chunking).
// overlap controls how many characters overlap between consecutive chunks.
func NewTextLoader(paths []string, chunkSize, overlap int) *TextLoader {
	return &TextLoader{
		paths:     paths,
		chunkSize: chunkSize,
		overlap:   overlap,
	}
}

// Load reads all files and returns documents, optionally chunked.
func (l *TextLoader) Load() ([]knowledge.Document, error) {
	var docs []knowledge.Document

	for _, p := range l.paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("text loader: reading %q: %w", p, err)
		}
		content := string(data)
		base := filepath.Base(p)

		if l.chunkSize <= 0 {
			docs = append(docs, knowledge.Document{
				ID:      docID(p, 0),
				Content: content,
				Metadata: map[string]any{
					"source": p,
					"name":   base,
					"type":   "text",
				},
			})
			continue
		}

		chunks := chunkText(content, l.chunkSize, l.overlap)
		for i, chunk := range chunks {
			docs = append(docs, knowledge.Document{
				ID:      docID(p, i),
				Content: chunk,
				Metadata: map[string]any{
					"source":    p,
					"name":      base,
					"type":      "text",
					"chunk_idx": i,
					"total":     len(chunks),
				},
			})
		}
	}

	return docs, nil
}

func chunkText(text string, size, overlap int) []string {
	if len(text) <= size {
		return []string{text}
	}
	var chunks []string
	step := size - overlap
	if step <= 0 {
		step = 1
	}
	for i := 0; i < len(text); i += step {
		end := i + size
		if end > len(text) {
			end = len(text)
		}
		chunks = append(chunks, text[i:end])
		if end == len(text) {
			break
		}
	}
	return chunks
}

func docID(path string, chunk int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", path, chunk)))
	return fmt.Sprintf("txt-%s", strings.ToLower(fmt.Sprintf("%x", h[:8])))
}
