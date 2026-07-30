package memory

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/storage"
)

// recallVocab is the fixed keyword vocabulary for the deterministic test
// embedder: each dimension flags the presence of one keyword, so a query that
// shares keywords with a stored memory scores higher. No network, fully
// reproducible.
var recallVocab = []string{
	"color", "blue", "food", "pizza", "city", "paris", "secret", "name", "alice", "bob",
}

type mockEmbedder struct{}

func (mockEmbedder) Embed(_ context.Context, req *model.EmbeddingRequest) (*model.EmbeddingResponse, error) {
	embs := make([][]float32, len(req.Input))
	for i, text := range req.Input {
		lower := strings.ToLower(text)
		v := make([]float32, len(recallVocab))
		for j, w := range recallVocab {
			if strings.Contains(lower, w) {
				v[j] = 1
			}
		}
		embs[i] = l2normalize(v)
	}
	return &model.EmbeddingResponse{Embeddings: embs}, nil
}

func l2normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	norm := float32(math.Sqrt(sum))
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = x / norm
	}
	return out
}

// mockVectorStore is an in-memory VectorStore that ranks by cosine similarity
// (dot product of normalized vectors).
type mockVectorStore struct {
	mu   sync.Mutex
	cols map[string]map[string]storage.Embedding
}

func newMockVectorStore() *mockVectorStore {
	return &mockVectorStore{cols: make(map[string]map[string]storage.Embedding)}
}

func (s *mockVectorStore) CreateCollection(_ context.Context, name string, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cols[name] == nil {
		s.cols[name] = make(map[string]storage.Embedding)
	}
	return nil
}

func (s *mockVectorStore) Upsert(_ context.Context, col string, embs []storage.Embedding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cols[col] == nil {
		s.cols[col] = make(map[string]storage.Embedding)
	}
	for _, e := range embs {
		s.cols[col][e.ID] = e
	}
	return nil
}

func (s *mockVectorStore) Search(_ context.Context, col string, query []float32, topK int, opts ...storage.SearchOption) ([]storage.SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cols[col]
	if !ok {
		return nil, errors.New("collection not found")
	}
	filter := storage.ApplySearchOptions(opts...).Filter
	res := make([]storage.SearchResult, 0, len(c))
	for _, e := range c {
		if !storage.MatchesFilter(e.Metadata, filter) {
			continue
		}
		res = append(res, storage.SearchResult{Embedding: e, Score: dot(query, e.Vector)})
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Score > res[j].Score })
	if topK > 0 && topK < len(res) {
		res = res[:topK]
	}
	return res, nil
}

func (s *mockVectorStore) Delete(_ context.Context, col string, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cols[col] == nil {
		return nil
	}
	for _, id := range ids {
		delete(s.cols[col], id)
	}
	return nil
}

func (s *mockVectorStore) Close() error { return nil }

func dot(a, b []float32) float32 {
	var sum float32
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		sum += a[i] * b[i]
	}
	return sum
}

// indexedManager builds a Manager for userID with a shared vector index, seeding
// nothing. Callers seed via the returned manager's remember tool or Recall.
func indexedManager(userID string, backend storage.Storage, vstore storage.VectorStore) *Manager {
	return NewManager("agent1", userID, NewStore("agent1", backend), &mockProvider{}).
		WithVectorIndex(mockEmbedder{}, vstore, "m", len(recallVocab))
}

func remember(t *testing.T, m *Manager, key, value string) {
	t.Helper()
	for _, mt := range m.MemoryTools() {
		if mt.Name == "remember" {
			if _, err := mt.Handler(context.Background(), map[string]any{"key": key, "value": value}); err != nil {
				t.Fatalf("remember %q: %v", key, err)
			}
			return
		}
	}
	t.Fatal("remember tool not found")
}

// TestManager_Recall_CrossSession proves a memory written by one manager is
// semantically recalled by a *fresh* manager (a new session/process) sharing the
// same backends, with no explicit list call.
func TestManager_Recall_CrossSession(t *testing.T) {
	backend := newMemStorage()
	vstore := newMockVectorStore()
	ctx := context.Background()

	// Session 1: write two memories.
	writer := indexedManager("alice", backend, vstore)
	remember(t, writer, "favorite_color", "blue")
	remember(t, writer, "favorite_food", "pizza")

	// Session 2: a brand-new manager over the same backends.
	reader := indexedManager("alice", backend, vstore)

	tests := []struct {
		name    string
		query   string
		wantKey string
		wantVal any
	}{
		{name: "color query recalls color memory", query: "what is my favorite color", wantKey: "favorite_color", wantVal: "blue"},
		{name: "food query recalls food memory", query: "what food do i like the most", wantKey: "favorite_food", wantVal: "pizza"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := reader.Recall(ctx, tc.query, 5)
			if err != nil {
				t.Fatalf("Recall: %v", err)
			}
			if len(got) == 0 {
				t.Fatalf("Recall returned no memories")
			}
			if got[0].Key != tc.wantKey {
				t.Errorf("top memory key = %q, want %q (scores: %+v)", got[0].Key, tc.wantKey, got)
			}
			if got[0].Value != tc.wantVal {
				t.Errorf("top memory value = %v, want %v", got[0].Value, tc.wantVal)
			}
		})
	}
}

// TestManager_Recall_TopKOrdering asserts results are ranked by descending
// relevance and honor topK.
func TestManager_Recall_TopKOrdering(t *testing.T) {
	backend := newMemStorage()
	vstore := newMockVectorStore()
	ctx := context.Background()

	m := indexedManager("alice", backend, vstore)
	remember(t, m, "favorite_color", "blue") // overlaps query on {color, blue}
	remember(t, m, "favorite_food", "pizza") // overlaps query on {food}
	remember(t, m, "home_city", "paris")     // no overlap with query

	got, err := m.Recall(ctx, "color blue food", 3)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Recall returned %d memories, want 3", len(got))
	}
	if got[0].Key != "favorite_color" {
		t.Errorf("most relevant = %q, want favorite_color", got[0].Key)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Score < got[i].Score {
			t.Errorf("results not sorted by descending score: %v", got)
		}
	}

	// topK caps the result count.
	capped, err := m.Recall(ctx, "color blue food", 2)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if len(capped) != 2 {
		t.Errorf("Recall(topK=2) returned %d, want 2", len(capped))
	}
}

// TestManager_Recall_NoIndex verifies recall is a graceful no-op without an
// attached vector index.
func TestManager_Recall_NoIndex(t *testing.T) {
	backend := newMemStorage()
	m := NewManager("agent1", "alice", NewStore("agent1", backend), &mockProvider{})
	if m.CanRecall() {
		t.Fatal("CanRecall() = true without a vector index")
	}
	got, err := m.Recall(context.Background(), "anything", 5)
	if err != nil {
		t.Fatalf("Recall: %v", err)
	}
	if got != nil {
		t.Errorf("Recall without index = %v, want nil", got)
	}
}

// TestManager_AutoIndex verifies ExtractMemories mirrors stored facts into the
// vector index with the correct ID and tenant scope.
func TestManager_AutoIndex(t *testing.T) {
	backend := newMemStorage()
	vstore := newMockVectorStore()
	ctx := context.Background()

	provider := &mockProvider{response: `[{"key":"favorite_color","value":"blue"}]`}
	m := NewManager("agent1", "alice", NewStore("agent1", backend), provider).
		WithVectorIndex(mockEmbedder{}, vstore, "m", len(recallVocab))

	if err := m.ExtractMemories(ctx, []model.Message{{Role: "user", Content: "I like blue"}}); err != nil {
		t.Fatalf("ExtractMemories: %v", err)
	}

	// The scope store computes the expected vector ID and tenant token.
	scoped := NewStoreForUser("agent1", "alice", backend)
	wantID := scoped.longTermID("favorite_color")
	wantScope := scoped.bucketToken()

	vstore.mu.Lock()
	defer vstore.mu.Unlock()
	col := vstore.cols["mem_agent1"]
	if col == nil {
		t.Fatal("collection mem_agent1 was not created")
	}
	e, ok := col[wantID]
	if !ok {
		t.Fatalf("expected vector ID %q not indexed; have %v", wantID, keysOf(col))
	}
	if e.Metadata["scope"] != wantScope {
		t.Errorf("vector scope = %v, want %v", e.Metadata["scope"], wantScope)
	}
	if e.Metadata["key"] != "favorite_color" {
		t.Errorf("vector key metadata = %v, want favorite_color", e.Metadata["key"])
	}
}

func keysOf(m map[string]storage.Embedding) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
