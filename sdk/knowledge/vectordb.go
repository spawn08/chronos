package knowledge

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/storage"
)

// VectorKnowledge implements Knowledge backed by a VectorStore and EmbeddingsProvider.
//
// Indexing scales to large corpora: documents are split into overlapping chunks
// (each chunk carries metadata linking it to its source document) and embedded
// in bounded batches. Concurrent Load calls are serialized so indexing state is
// never corrupted or double-applied. Retrieval supports a configurable top-k and
// relevance threshold, and query embeddings are served from a bounded LRU+TTL
// cache so repeated queries are not re-embedded.
type VectorKnowledge struct {
	Collection string
	Dimension  int
	Store      storage.VectorStore
	Embedder   model.EmbeddingsProvider
	EmbedModel string

	embedBatchSize int
	chunkSize      int
	chunkOverlap   int
	defaultTopK    int
	scoreThreshold float32

	mu        sync.Mutex // guards documents and serializes Load
	documents []Document // raw documents to index

	queryCache *queryCache
}

// NewVectorKnowledge creates a vector-backed knowledge base. Behavior can be
// tuned with functional options; sensible defaults are applied when none are
// given (batched embedding, document chunking, a query-embedding cache, and a
// default top-k for retrieval).
func NewVectorKnowledge(collection string, dimension int, store storage.VectorStore, embedder model.EmbeddingsProvider, embedModel string, opts ...Option) *VectorKnowledge {
	v := &VectorKnowledge{
		Collection:     collection,
		Dimension:      dimension,
		Store:          store,
		Embedder:       embedder,
		EmbedModel:     embedModel,
		embedBatchSize: defaultEmbedBatchSize,
		chunkSize:      defaultChunkSize,
		chunkOverlap:   defaultChunkOverlap,
		defaultTopK:    defaultTopK,
		scoreThreshold: defaultScoreThreshold,
		queryCache:     newQueryCache(defaultQueryCacheSize, defaultQueryCacheTTL),
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// AddDocuments queues documents for indexing on next Load() call.
func (v *VectorKnowledge) AddDocuments(docs ...Document) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.documents = append(v.documents, docs...)
}

// Load creates the collection and indexes all queued documents. It is
// idempotent and safe to call concurrently: calls are serialized so indexing
// state cannot be corrupted or double-applied.
func (v *VectorKnowledge) Load(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if err := v.Store.CreateCollection(ctx, v.Collection, v.Dimension); err != nil {
		return fmt.Errorf("knowledge load: create collection: %w", err)
	}

	if len(v.documents) == 0 {
		return nil
	}

	// Split documents into chunks before embedding so large docs do not exceed
	// model context limits and index at a useful granularity.
	var chunks []Document
	for _, d := range v.documents {
		chunks = append(chunks, v.chunkDocument(d)...)
	}

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}

	vectors, err := v.embedBatched(ctx, texts)
	if err != nil {
		return fmt.Errorf("knowledge load: embed: %w", err)
	}

	embeddings := make([]storage.Embedding, len(chunks))
	for i, c := range chunks {
		embeddings[i] = storage.Embedding{
			ID:       c.ID,
			Vector:   vectors[i],
			Metadata: c.Metadata,
			Content:  c.Content,
		}
	}

	if err := v.Store.Upsert(ctx, v.Collection, embeddings); err != nil {
		return fmt.Errorf("knowledge load: upsert: %w", err)
	}
	return nil
}

// chunkDocument splits a document's content into overlapping chunks. A document
// that fits within the chunk size is returned as a single chunk that preserves
// the original identity and metadata. Split chunks receive deterministic IDs and
// metadata linking them back to their source document.
func (v *VectorKnowledge) chunkDocument(d Document) []Document {
	baseID := d.ID
	if baseID == "" {
		h := sha256.Sum256([]byte(d.Content))
		baseID = fmt.Sprintf("%x", h[:16])
	}

	parts := splitChunks(d.Content, v.chunkSize, v.chunkOverlap)
	if len(parts) <= 1 {
		return []Document{{ID: baseID, Content: d.Content, Metadata: d.Metadata}}
	}

	out := make([]Document, len(parts))
	for i, p := range parts {
		meta := make(map[string]any, len(d.Metadata)+3)
		for k, val := range d.Metadata {
			meta[k] = val
		}
		meta["source_doc_id"] = baseID
		meta["chunk_index"] = i
		meta["chunk_count"] = len(parts)
		out[i] = Document{
			ID:       fmt.Sprintf("%s#%d", baseID, i),
			Content:  p,
			Metadata: meta,
		}
	}
	return out
}

// embedBatched embeds texts in bounded batches so a large corpus does not
// require a single oversized embedding request.
func (v *VectorKnowledge) embedBatched(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	batch := v.embedBatchSize
	if batch <= 0 {
		batch = len(texts)
	}

	for start := 0; start < len(texts); start += batch {
		end := start + batch
		if end > len(texts) {
			end = len(texts)
		}
		resp, err := v.Embedder.Embed(ctx, &model.EmbeddingRequest{
			Model: v.EmbedModel,
			Input: texts[start:end],
		})
		if err != nil {
			return nil, fmt.Errorf("embed batch [%d:%d]: %w", start, end, err)
		}
		if len(resp.Embeddings) != end-start {
			return nil, fmt.Errorf("embed batch [%d:%d]: expected %d embeddings, got %d", start, end, end-start, len(resp.Embeddings))
		}
		out = append(out, resp.Embeddings...)
	}
	return out, nil
}

// Search embeds the query and performs similarity search. When topK <= 0 the
// configured default is used. Results below the configured score threshold are
// dropped. Query embeddings are served from the cache when available.
func (v *VectorKnowledge) Search(ctx context.Context, query string, topK int) ([]Document, error) {
	if topK <= 0 {
		topK = v.defaultTopK
	}

	vector, err := v.embedQuery(ctx, query)
	if err != nil {
		return nil, err
	}

	results, err := v.Store.Search(ctx, v.Collection, vector, topK)
	if err != nil {
		return nil, fmt.Errorf("knowledge search: %w", err)
	}

	docs := make([]Document, 0, len(results))
	for _, r := range results {
		if v.scoreThreshold > 0 && r.Score < v.scoreThreshold {
			continue
		}
		docs = append(docs, Document{
			ID:       r.ID,
			Content:  r.Content,
			Metadata: r.Metadata,
			Score:    r.Score,
		})
	}
	return docs, nil
}

// embedQuery returns the embedding for a query, consulting and populating the
// bounded LRU+TTL cache when it is enabled.
func (v *VectorKnowledge) embedQuery(ctx context.Context, query string) ([]float32, error) {
	key := v.EmbedModel + ":" + query
	if v.queryCache != nil {
		if vec, ok := v.queryCache.get(key); ok {
			return vec, nil
		}
	}

	resp, err := v.Embedder.Embed(ctx, &model.EmbeddingRequest{
		Model: v.EmbedModel,
		Input: []string{query},
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge search: embed query: %w", err)
	}
	if len(resp.Embeddings) == 0 {
		return nil, fmt.Errorf("knowledge search: embed query: empty embedding response")
	}

	vec := resp.Embeddings[0]
	if v.queryCache != nil {
		v.queryCache.put(key, vec)
	}
	return vec, nil
}

func (v *VectorKnowledge) Close() error {
	return v.Store.Close()
}

// splitChunks splits text into overlapping chunks of at most size runes. When
// size <= 0 or the text fits in a single chunk, the original text is returned
// unchanged as a single chunk. Overlap is clamped to [0, size).
func splitChunks(text string, size, overlap int) []string {
	runes := []rune(text)
	if size <= 0 || len(runes) <= size {
		return []string{text}
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= size {
		overlap = size / 2
	}

	step := size - overlap
	var chunks []string
	for start := 0; start < len(runes); start += step {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}
