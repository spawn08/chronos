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

// SearchOptions configures a similarity search. It is built from the variadic
// SearchOption arguments to VectorStore.Search via ApplySearchOptions.
type SearchOptions struct {
	// Filter restricts results to embeddings whose metadata matches every
	// key/value pair (exact match, AND semantics). A nil or empty Filter matches
	// everything. Adapters that store metadata structurally (qdrant, pgvector,
	// pinecone, chromadb) apply it server-side so top-k is computed over the
	// matching subset; adapters that store metadata as an opaque blob (lancedb,
	// milvus, weaviate, redisvector) apply it client-side over the returned
	// window (which may under-return under a highly selective filter).
	Filter map[string]any
}

// SearchOption customizes a similarity search (e.g. a metadata filter).
type SearchOption func(*SearchOptions)

// WithFilter restricts a search to embeddings whose metadata matches every
// key/value pair in filter (exact match). This is how callers scope a shared
// collection — e.g. a tenant token — so top-k is computed within that scope.
func WithFilter(filter map[string]any) SearchOption {
	return func(o *SearchOptions) { o.Filter = filter }
}

// ApplySearchOptions folds opts into a SearchOptions. Adapters call this at the
// top of Search to read the requested filter.
func ApplySearchOptions(opts ...SearchOption) SearchOptions {
	var o SearchOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// FilterSearchResults returns, in order, the results whose metadata matches
// filter. A nil or empty filter returns results unchanged. This is the shared
// client-side path for adapters that cannot filter on metadata server-side. It
// returns a new slice and never mutates the input.
func FilterSearchResults(results []SearchResult, filter map[string]any) []SearchResult {
	if len(filter) == 0 {
		return results
	}
	out := make([]SearchResult, 0, len(results))
	for _, r := range results {
		if MatchesFilter(r.Metadata, filter) {
			out = append(out, r)
		}
	}
	return out
}

// MatchesFilter reports whether md satisfies every key/value pair in filter
// (exact match). A nil or empty filter matches everything. It is the shared
// client-side implementation for in-memory stores and adapters without native
// metadata filtering.
func MatchesFilter(md, filter map[string]any) bool {
	for k, v := range filter {
		if md[k] != v {
			return false
		}
	}
	return true
}

// VectorStore abstracts vector DB operations for RAG and embeddings.
type VectorStore interface {
	// Upsert inserts or updates embeddings in the given collection.
	Upsert(ctx context.Context, collection string, embeddings []Embedding) error

	// Search performs similarity search and returns top-k results. Optional
	// SearchOptions (e.g. WithFilter) scope the search; adapters apply any filter
	// so that top-k is selected over the matching subset.
	Search(ctx context.Context, collection string, query []float32, topK int, opts ...SearchOption) ([]SearchResult, error)

	// Delete removes embeddings by IDs.
	Delete(ctx context.Context, collection string, ids []string) error

	// CreateCollection ensures a collection exists.
	CreateCollection(ctx context.Context, name string, dimension int) error

	// Close releases resources.
	Close() error
}
