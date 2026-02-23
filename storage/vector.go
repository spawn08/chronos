package storage

import "context"

// Embedding represents a vector embedding with metadata.
type Embedding struct {
	ID       string         `json:"id"`
	Vector   []float32      `json:"vector"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Content  string         `json:"content,omitempty"`
}

// SearchResult is a single result from a similarity search.
type SearchResult struct {
	Embedding
	Score float32 `json:"score"`
}

// VectorStore abstracts vector DB operations for RAG and embeddings.
type VectorStore interface {
	// Upsert inserts or updates embeddings in the given collection.
	Upsert(ctx context.Context, collection string, embeddings []Embedding) error

	// Search performs similarity search and returns top-k results.
	Search(ctx context.Context, collection string, query []float32, topK int) ([]SearchResult, error)

	// Delete removes embeddings by IDs.
	Delete(ctx context.Context, collection string, ids []string) error

	// CreateCollection ensures a collection exists.
	CreateCollection(ctx context.Context, name string, dimension int) error

	// Close releases resources.
	Close() error
}
