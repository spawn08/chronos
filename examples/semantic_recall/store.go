package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/spawn08/chronos/storage"
)

// memoryVectorStore is a tiny, self-contained storage.VectorStore backed by a
// map and cosine similarity. It exists so this example runs offline; it is NOT
// meant for production — use the Qdrant adapter (storage/adapters/qdrant) for
// real workloads.
type memoryVectorStore struct {
	mu          sync.RWMutex
	collections map[string]map[string]storage.Embedding
}

func newMemoryVectorStore() *memoryVectorStore {
	return &memoryVectorStore{collections: make(map[string]map[string]storage.Embedding)}
}

func (m *memoryVectorStore) CreateCollection(_ context.Context, name string, _ int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.collections[name]; !ok {
		m.collections[name] = make(map[string]storage.Embedding)
	}
	return nil
}

func (m *memoryVectorStore) Upsert(_ context.Context, collection string, embeddings []storage.Embedding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	col, ok := m.collections[collection]
	if !ok {
		return fmt.Errorf("collection %q does not exist", collection)
	}
	for _, e := range embeddings {
		col[e.ID] = e
	}
	return nil
}

func (m *memoryVectorStore) Search(_ context.Context, collection string, query []float32, topK int, opts ...storage.SearchOption) ([]storage.SearchResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	col, ok := m.collections[collection]
	if !ok {
		return nil, fmt.Errorf("collection %q does not exist", collection)
	}

	// Apply any metadata filter before ranking so top-k is over the subset.
	filter := storage.ApplySearchOptions(opts...).Filter
	results := make([]storage.SearchResult, 0, len(col))
	for _, e := range col {
		if !storage.MatchesFilter(e.Metadata, filter) {
			continue
		}
		results = append(results, storage.SearchResult{Embedding: e, Score: cosine(query, e.Vector)})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })

	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

func (m *memoryVectorStore) Delete(_ context.Context, collection string, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if col, ok := m.collections[collection]; ok {
		for _, id := range ids {
			delete(col, id)
		}
	}
	return nil
}

func (m *memoryVectorStore) Close() error { return nil }

// cosine returns the cosine similarity of two vectors, or 0 for mismatched or
// zero-magnitude inputs.
func cosine(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}
