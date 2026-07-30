package knowledge

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/storage"
)

// ---- Instrumented fakes ----

// countingEmbedder records each Embed call and the batch size it received.
type countingEmbedder struct {
	mu         sync.Mutex
	calls      int
	batchSizes []int
	dim        int
}

func (e *countingEmbedder) Embed(_ context.Context, req *model.EmbeddingRequest) (*model.EmbeddingResponse, error) {
	e.mu.Lock()
	e.calls++
	e.batchSizes = append(e.batchSizes, len(req.Input))
	e.mu.Unlock()

	dim := e.dim
	if dim == 0 {
		dim = 3
	}
	embs := make([][]float32, len(req.Input))
	for i := range embs {
		vec := make([]float32, dim)
		for j := range vec {
			vec[j] = 0.1
		}
		embs[i] = vec
	}
	return &model.EmbeddingResponse{Embeddings: embs}, nil
}

func (e *countingEmbedder) callCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func (e *countingEmbedder) sizes() []int {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]int, len(e.batchSizes))
	copy(out, e.batchSizes)
	return out
}

// recordingStore is a concurrency-safe VectorStore that records upserts.
type recordingStore struct {
	mu       sync.Mutex
	upserted []storage.Embedding
	results  []storage.SearchResult
}

func (s *recordingStore) CreateCollection(_ context.Context, _ string, _ int) error { return nil }
func (s *recordingStore) Upsert(_ context.Context, _ string, embeddings []storage.Embedding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upserted = append(s.upserted, embeddings...)
	return nil
}
func (s *recordingStore) Search(_ context.Context, _ string, _ []float32, topK int, _ ...storage.SearchOption) ([]storage.SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := s.results
	if topK > 0 && topK < len(res) {
		res = res[:topK]
	}
	out := make([]storage.SearchResult, len(res))
	copy(out, res)
	return out, nil
}
func (s *recordingStore) Delete(_ context.Context, _ string, _ []string) error { return nil }
func (s *recordingStore) Close() error                                         { return nil }

func (s *recordingStore) upsertCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.upserted)
}

// ---- Batching ----

func TestLoadBatching(t *testing.T) {
	tests := []struct {
		name       string
		numDocs    int
		batchSize  int
		wantCalls  int
		wantSizes  []int
		wantUpsert int
	}{
		{
			name:       "even batches",
			numDocs:    10,
			batchSize:  5,
			wantCalls:  2,
			wantSizes:  []int{5, 5},
			wantUpsert: 10,
		},
		{
			name:       "trailing partial batch",
			numDocs:    7,
			batchSize:  3,
			wantCalls:  3,
			wantSizes:  []int{3, 3, 1},
			wantUpsert: 7,
		},
		{
			name:       "single batch fits all",
			numDocs:    4,
			batchSize:  100,
			wantCalls:  1,
			wantSizes:  []int{4},
			wantUpsert: 4,
		},
		{
			name:       "batch disabled embeds all at once",
			numDocs:    6,
			batchSize:  0,
			wantCalls:  1,
			wantSizes:  []int{6},
			wantUpsert: 6,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			emb := &countingEmbedder{}
			store := &recordingStore{}
			// Disable chunking so each doc maps to exactly one chunk.
			vk := NewVectorKnowledge("col", 3, store, emb, "model",
				WithEmbedBatchSize(tc.batchSize),
				WithChunking(0, 0),
			)
			for i := 0; i < tc.numDocs; i++ {
				vk.AddDocuments(Document{ID: fmt.Sprintf("d%d", i), Content: fmt.Sprintf("content %d", i)})
			}

			if err := vk.Load(context.Background()); err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := emb.callCount(); got != tc.wantCalls {
				t.Errorf("embed calls: got %d, want %d", got, tc.wantCalls)
			}
			if got := emb.sizes(); !equalInts(got, tc.wantSizes) {
				t.Errorf("batch sizes: got %v, want %v", got, tc.wantSizes)
			}
			if got := store.upsertCount(); got != tc.wantUpsert {
				t.Errorf("upserted: got %d, want %d", got, tc.wantUpsert)
			}
		})
	}
}

// TestLoadLargeCorpus indexes a large corpus and asserts no single embed call
// exceeds the batch cap — the acceptance criterion that indexing "does not fail
// on a single embed call".
func TestLoadLargeCorpus(t *testing.T) {
	const numDocs = 1000
	const batch = defaultEmbedBatchSize // exercise the on-by-default batch size

	emb := &countingEmbedder{}
	store := &recordingStore{}
	// Disable chunking so each document maps to exactly one chunk.
	vk := NewVectorKnowledge("col", 3, store, emb, "model", WithChunking(0, 0))
	for i := 0; i < numDocs; i++ {
		vk.AddDocuments(Document{ID: fmt.Sprintf("d%d", i), Content: fmt.Sprintf("content %d", i)})
	}

	if err := vk.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	wantCalls := (numDocs + batch - 1) / batch
	if got := emb.callCount(); got != wantCalls {
		t.Errorf("embed calls: got %d, want %d", got, wantCalls)
	}
	for i, sz := range emb.sizes() {
		if sz > batch {
			t.Errorf("embed call %d had size %d, exceeds batch cap %d", i, sz, batch)
		}
	}
	if got := store.upsertCount(); got != numDocs {
		t.Errorf("upserted: got %d, want %d", got, numDocs)
	}
}

// TestLoadDrainsQueue pins the drain contract: a Load with no newly-added
// documents does not re-embed or re-upsert already-indexed documents, and a
// later Load indexes only the documents added since the previous Load.
func TestLoadDrainsQueue(t *testing.T) {
	emb := &countingEmbedder{}
	store := &recordingStore{}
	vk := NewVectorKnowledge("col", 3, store, emb, "model", WithChunking(0, 0))

	for i := 0; i < 5; i++ {
		vk.AddDocuments(Document{ID: fmt.Sprintf("d%d", i), Content: fmt.Sprintf("content %d", i)})
	}
	if err := vk.Load(context.Background()); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	callsAfterFirst := emb.callCount()
	if got := store.upsertCount(); got != 5 {
		t.Fatalf("after first Load upserted: got %d, want 5", got)
	}

	// A second Load with an empty queue must be a no-op for embedding/upsert.
	if err := vk.Load(context.Background()); err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if got := emb.callCount(); got != callsAfterFirst {
		t.Errorf("empty re-Load embedded again: got %d calls, want %d", got, callsAfterFirst)
	}
	if got := store.upsertCount(); got != 5 {
		t.Errorf("empty re-Load re-upserted: got %d, want 5", got)
	}

	// Adding one more document and reloading indexes only that document.
	vk.AddDocuments(Document{ID: "d5", Content: "content 5"})
	if err := vk.Load(context.Background()); err != nil {
		t.Fatalf("third Load: %v", err)
	}
	if got := emb.callCount(); got != callsAfterFirst+1 {
		t.Errorf("incremental Load embed calls: got %d, want %d", got, callsAfterFirst+1)
	}
	if got := store.upsertCount(); got != 6 {
		t.Errorf("incremental Load upserted: got %d, want 6", got)
	}
}

// ---- Chunking ----

func TestLoadChunking(t *testing.T) {
	emb := &countingEmbedder{}
	store := &recordingStore{}
	// chunkSize 100, overlap 20 -> a 250-rune doc splits into multiple chunks.
	vk := NewVectorKnowledge("col", 3, store, emb, "model",
		WithChunking(100, 20),
		WithEmbedBatchSize(1000),
	)
	longContent := strings.Repeat("a", 250)
	vk.AddDocuments(Document{ID: "big", Content: longContent, Metadata: map[string]any{"origin": "test"}})

	if err := vk.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if store.upsertCount() <= 1 {
		t.Fatalf("expected long doc to split into multiple chunks, got %d", store.upsertCount())
	}

	// Every chunk must link back to its source document and preserve metadata.
	for i, e := range store.upserted {
		if e.Metadata["source_doc_id"] != "big" {
			t.Errorf("chunk %d: source_doc_id=%v, want 'big'", i, e.Metadata["source_doc_id"])
		}
		if e.Metadata["origin"] != "test" {
			t.Errorf("chunk %d: original metadata not carried, got %v", i, e.Metadata)
		}
		if e.Metadata["chunk_index"] != i {
			t.Errorf("chunk %d: chunk_index=%v, want %d", i, e.Metadata["chunk_index"], i)
		}
		if !strings.HasPrefix(e.ID, "big#") {
			t.Errorf("chunk %d: ID=%q, want prefix 'big#'", i, e.ID)
		}
	}
}

func TestLoadShortDocNotChunked(t *testing.T) {
	emb := &countingEmbedder{}
	store := &recordingStore{}
	vk := NewVectorKnowledge("col", 3, store, emb, "model", WithChunking(100, 20))
	vk.AddDocuments(Document{ID: "small", Content: "short content", Metadata: map[string]any{"k": "v"}})

	if err := vk.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if store.upsertCount() != 1 {
		t.Fatalf("expected single chunk, got %d", store.upsertCount())
	}
	// Identity preserved for unsplit docs.
	if store.upserted[0].ID != "small" {
		t.Errorf("ID: got %q, want 'small'", store.upserted[0].ID)
	}
	if store.upserted[0].Metadata["k"] != "v" {
		t.Errorf("metadata not preserved: %v", store.upserted[0].Metadata)
	}
	if _, ok := store.upserted[0].Metadata["chunk_index"]; ok {
		t.Errorf("unsplit doc should not carry chunk metadata: %v", store.upserted[0].Metadata)
	}
}

func TestSplitChunks(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		size    int
		overlap int
		wantLen int
	}{
		{"fits in one chunk", "hello", 100, 10, 1},
		{"disabled chunking", strings.Repeat("x", 500), 0, 0, 1},
		{"splits with overlap", strings.Repeat("x", 100), 40, 10, 3},
		{"overlap clamped to half", strings.Repeat("x", 30), 10, 100, 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := splitChunks(tc.text, tc.size, tc.overlap)
			if len(got) != tc.wantLen {
				t.Errorf("chunk count: got %d, want %d", len(got), tc.wantLen)
			}
			// Reassembling first chars of each chunk must reproduce all content.
			if tc.size > 0 && len(tc.text) > tc.size {
				for _, c := range got {
					if len([]rune(c)) > tc.size {
						t.Errorf("chunk exceeds size %d: %d", tc.size, len([]rune(c)))
					}
				}
			}
		})
	}
}

// ---- Query cache ----

func TestSearchQueryCache(t *testing.T) {
	emb := &countingEmbedder{}
	store := &recordingStore{
		results: []storage.SearchResult{
			{Embedding: storage.Embedding{ID: "r1", Content: "c"}, Score: 0.9},
		},
	}
	vk := NewVectorKnowledge("col", 3, store, emb, "model")

	for i := 0; i < 3; i++ {
		if _, err := vk.Search(context.Background(), "same query", 5); err != nil {
			t.Fatalf("Search: %v", err)
		}
	}
	if got := emb.callCount(); got != 1 {
		t.Errorf("expected query embedded once (cached), got %d embed calls", got)
	}

	// A different query triggers a fresh embed.
	if _, err := vk.Search(context.Background(), "other query", 5); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := emb.callCount(); got != 2 {
		t.Errorf("expected 2 embed calls after new query, got %d", got)
	}
}

func TestSearchCacheDisabled(t *testing.T) {
	emb := &countingEmbedder{}
	store := &recordingStore{}
	vk := NewVectorKnowledge("col", 3, store, emb, "model", WithoutQueryCache())

	for i := 0; i < 3; i++ {
		if _, err := vk.Search(context.Background(), "same query", 5); err != nil {
			t.Fatalf("Search: %v", err)
		}
	}
	if got := emb.callCount(); got != 3 {
		t.Errorf("cache disabled: expected 3 embed calls, got %d", got)
	}
}

func TestQueryCacheTTLAndLRU(t *testing.T) {
	c := newQueryCache(2, time.Minute)
	current := time.Unix(0, 0)
	c.now = func() time.Time { return current }

	c.put("a", []float32{1})
	if _, ok := c.get("a"); !ok {
		t.Fatal("expected 'a' present")
	}

	// Expire via TTL.
	current = current.Add(2 * time.Minute)
	if _, ok := c.get("a"); ok {
		t.Error("expected 'a' expired")
	}

	// LRU eviction at capacity 2.
	current = time.Unix(0, 0)
	c = newQueryCache(2, time.Minute)
	c.now = func() time.Time { return current }
	c.put("a", []float32{1})
	c.put("b", []float32{2})
	_, _ = c.get("a")        // touch 'a' so 'b' is LRU
	c.put("c", []float32{3}) // evicts 'b'
	if _, ok := c.get("b"); ok {
		t.Error("expected 'b' evicted")
	}
	if _, ok := c.get("a"); !ok {
		t.Error("expected 'a' retained")
	}
	if _, ok := c.get("c"); !ok {
		t.Error("expected 'c' present")
	}
	if c.len() != 2 {
		t.Errorf("cache len: got %d, want 2", c.len())
	}
}

func TestNewQueryCacheDisabled(t *testing.T) {
	if newQueryCache(0, time.Minute) != nil {
		t.Error("expected nil cache for capacity 0")
	}
	if newQueryCache(10, 0) != nil {
		t.Error("expected nil cache for ttl 0")
	}
}

// ---- Top-k / threshold ----

func TestSearchTopKAndThreshold(t *testing.T) {
	results := []storage.SearchResult{
		{Embedding: storage.Embedding{ID: "r1"}, Score: 0.9},
		{Embedding: storage.Embedding{ID: "r2"}, Score: 0.6},
		{Embedding: storage.Embedding{ID: "r3"}, Score: 0.3},
		{Embedding: storage.Embedding{ID: "r4"}, Score: 0.1},
	}

	tests := []struct {
		name      string
		topK      int
		threshold float32
		wantIDs   []string
	}{
		{"no threshold keeps all", 10, 0, []string{"r1", "r2", "r3", "r4"}},
		{"threshold drops low scores", 10, 0.5, []string{"r1", "r2"}},
		{"topK limits before filter", 2, 0, []string{"r1", "r2"}},
		{"default topK when zero", 0, 0.2, []string{"r1", "r2", "r3"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			emb := &countingEmbedder{}
			store := &recordingStore{results: results}
			opts := []Option{WithScoreThreshold(tc.threshold)}
			if tc.name == "default topK when zero" {
				opts = append(opts, WithTopK(10))
			}
			vk := NewVectorKnowledge("col", 3, store, emb, "model", opts...)

			docs, err := vk.Search(context.Background(), "q", tc.topK)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			var gotIDs []string
			for _, d := range docs {
				gotIDs = append(gotIDs, d.ID)
			}
			if !equalStrings(gotIDs, tc.wantIDs) {
				t.Errorf("ids: got %v, want %v", gotIDs, tc.wantIDs)
			}
		})
	}
}

// ---- Concurrency (-race) ----

func TestConcurrentLoadAndSearch(t *testing.T) {
	emb := &countingEmbedder{}
	store := &recordingStore{
		results: []storage.SearchResult{
			{Embedding: storage.Embedding{ID: "r1"}, Score: 0.9},
		},
	}
	vk := NewVectorKnowledge("col", 3, store, emb, "model", WithChunking(50, 10))

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			vk.AddDocuments(Document{ID: fmt.Sprintf("d%d", n), Content: strings.Repeat("x", 120)})
			if err := vk.Load(context.Background()); err != nil {
				t.Errorf("Load: %v", err)
			}
		}(i)

		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if _, err := vk.Search(context.Background(), fmt.Sprintf("q%d", n), 3); err != nil {
				t.Errorf("Search: %v", err)
			}
		}(i)
	}
	wg.Wait()

	// With the queue drained on each Load, every one of the 8 documents is
	// indexed exactly once regardless of how the concurrent Add/Load calls
	// interleave. Each 120-rune doc splits into 3 chunks (size 50, overlap 10,
	// step 40 → [0:50],[40:90],[80:120]), so the store sees exactly 24 upserts —
	// proving race-free *and* no double-apply.
	const wantUpserts = 8 * 3
	if got := store.upsertCount(); got != wantUpserts {
		t.Errorf("concurrent indexing upserted %d chunks, want %d (documents re-indexed?)", got, wantUpserts)
	}
}

// ---- helpers ----

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
