// Package knowledge provides a RAG knowledge base abstraction inspired by Agno's Knowledge protocol.
package knowledge

import "context"

// Document represents a retrieved knowledge document.
type Document struct {
	ID       string         `json:"id"`
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Score    float32        `json:"score,omitempty"`
}

// Knowledge is the interface for pluggable knowledge sources.
// Implementations load documents, index them, and serve similarity searches.
type Knowledge interface {
	// Load indexes all documents into the underlying store. Idempotent.
	Load(ctx context.Context) error

	// Search returns the top-k most relevant documents for the query.
	Search(ctx context.Context, query string, topK int) ([]Document, error)

	// Close releases resources.
	Close() error
}
