package knowledge

import "time"

// Default tuning values for VectorKnowledge. They are chosen to preserve the
// historical single-batch, single-chunk behavior for small corpora while
// enabling safe scaling for large ones.
const (
	defaultEmbedBatchSize = 64
	defaultChunkSize      = 1000
	defaultChunkOverlap   = 100
	defaultTopK           = 5
	defaultScoreThreshold = 0 // 0 disables threshold filtering
	defaultQueryCacheSize = 256
	defaultQueryCacheTTL  = 5 * time.Minute
)

// Option configures a VectorKnowledge at construction time. Options are applied
// in order, so later options override earlier ones.
type Option func(*VectorKnowledge)

// WithEmbedBatchSize bounds how many chunks are embedded per provider call
// during Load. A value <= 0 embeds everything in a single call.
func WithEmbedBatchSize(n int) Option {
	return func(v *VectorKnowledge) { v.embedBatchSize = n }
}

// WithChunking configures document chunking used during indexing. Documents
// longer than size (measured in runes) are split into overlapping chunks. A
// size <= 0 disables chunking. Overlap is clamped to [0, size).
func WithChunking(size, overlap int) Option {
	return func(v *VectorKnowledge) {
		v.chunkSize = size
		v.chunkOverlap = overlap
	}
}

// WithTopK sets the default number of results returned by Search when the
// caller passes topK <= 0.
func WithTopK(k int) Option {
	return func(v *VectorKnowledge) { v.defaultTopK = k }
}

// WithScoreThreshold drops search results whose relevance score is below the
// threshold. A value <= 0 keeps all results.
func WithScoreThreshold(threshold float32) Option {
	return func(v *VectorKnowledge) { v.scoreThreshold = threshold }
}

// WithQueryCache configures the bounded LRU+TTL cache for query embeddings. A
// capacity <= 0 or ttl <= 0 disables the cache.
func WithQueryCache(capacity int, ttl time.Duration) Option {
	return func(v *VectorKnowledge) {
		v.queryCache = newQueryCache(capacity, ttl)
	}
}

// WithoutQueryCache disables query-embedding caching.
func WithoutQueryCache() Option {
	return func(v *VectorKnowledge) { v.queryCache = nil }
}
