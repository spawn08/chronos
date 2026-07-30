package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/memory"
	"github.com/spawn08/chronos/storage"
)

// constEmbedder maps every input to the same unit vector. Similarity is
// therefore uniform, so these tests exercise recall *wiring* and tenant scoping
// rather than ranking (ranking is covered in sdk/memory/recall_test.go).
type constEmbedder struct{}

func (constEmbedder) Embed(_ context.Context, req *model.EmbeddingRequest) (*model.EmbeddingResponse, error) {
	embs := make([][]float32, len(req.Input))
	for i := range embs {
		embs[i] = []float32{1, 0, 0, 0}
	}
	return &model.EmbeddingResponse{Embeddings: embs}, nil
}

// recallVectorStore is a minimal in-memory VectorStore returning every stored
// embedding in the collection (score 1) — enough to drive recall injection.
type recallVectorStore struct {
	mu   sync.Mutex
	cols map[string]map[string]storage.Embedding
}

func newRecallVectorStore() *recallVectorStore {
	return &recallVectorStore{cols: make(map[string]map[string]storage.Embedding)}
}

func (s *recallVectorStore) CreateCollection(_ context.Context, name string, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cols[name] == nil {
		s.cols[name] = make(map[string]storage.Embedding)
	}
	return nil
}

func (s *recallVectorStore) Upsert(_ context.Context, col string, embs []storage.Embedding) error {
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

func (s *recallVectorStore) Search(_ context.Context, col string, _ []float32, topK int, opts ...storage.SearchOption) ([]storage.SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cols[col]
	filter := storage.ApplySearchOptions(opts...).Filter
	res := make([]storage.SearchResult, 0, len(c))
	for _, e := range c {
		if !storage.MatchesFilter(e.Metadata, filter) {
			continue
		}
		res = append(res, storage.SearchResult{Embedding: e, Score: 1})
	}
	if topK > 0 && topK < len(res) {
		res = res[:topK]
	}
	return res, nil
}

func (s *recallVectorStore) Delete(_ context.Context, col string, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		delete(s.cols[col], id)
	}
	return nil
}

func (s *recallVectorStore) Close() error { return nil }

func rememberFact(t *testing.T, m *memory.Manager, key, value string) {
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

func systemContents(msgs []model.Message) string {
	var b strings.Builder
	for i := range msgs {
		if msgs[i].Role == model.RoleSystem {
			b.WriteString(msgs[i].Content)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// TestAgent_SemanticRecall_Injected verifies that when recall is enabled (the
// default) and the manager has a vector index, the relevant memory is injected
// into the chat request via the semantic-recall path.
func TestAgent_SemanticRecall_Injected(t *testing.T) {
	store := newTestStorage()
	vstore := newRecallVectorStore()
	mgr := memory.NewManager("a1", "alice", memory.NewStore("a1", store), &testProvider{response: &model.ChatResponse{Content: "[]"}}).
		WithVectorIndex(constEmbedder{}, vstore, "m", 4)
	rememberFact(t, mgr, "favorite_color", "blue")

	a, err := New("a1", "Test").
		WithModel(&testProvider{response: &model.ChatResponse{Content: "hi"}}).
		WithMemoryManager(mgr).
		WithUserID("alice").
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	req, _, err := a.buildChatRequest(context.Background(), "what is my favorite color?")
	if err != nil {
		t.Fatalf("buildChatRequest: %v", err)
	}
	sys := systemContents(req.Messages)
	if !strings.Contains(sys, "Relevant user memories:") {
		t.Errorf("expected semantic-recall header in system context, got:\n%s", sys)
	}
	if !strings.Contains(sys, "favorite_color: blue") {
		t.Errorf("expected recalled memory in system context, got:\n%s", sys)
	}
}

// TestAgent_Recall_Toggle verifies that disabling recall falls back to the
// legacy full-memory dump, preserving prior behavior.
func TestAgent_Recall_Toggle(t *testing.T) {
	store := newTestStorage()
	vstore := newRecallVectorStore()
	mgr := memory.NewManager("a1", "alice", memory.NewStore("a1", store), &testProvider{response: &model.ChatResponse{Content: "[]"}}).
		WithVectorIndex(constEmbedder{}, vstore, "m", 4)
	rememberFact(t, mgr, "favorite_color", "blue")

	a, err := New("a1", "Test").
		WithModel(&testProvider{response: &model.ChatResponse{Content: "hi"}}).
		WithMemoryManager(mgr).
		WithUserID("alice").
		WithMemoryRecall(RecallConfig{Disabled: true}).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	req, _, err := a.buildChatRequest(context.Background(), "what is my favorite color?")
	if err != nil {
		t.Fatalf("buildChatRequest: %v", err)
	}
	sys := systemContents(req.Messages)
	if !strings.Contains(sys, "User memories:") {
		t.Errorf("expected legacy dump header when recall disabled, got:\n%s", sys)
	}
	if strings.Contains(sys, "Relevant user memories:") {
		t.Errorf("recall path ran despite Disabled=true:\n%s", sys)
	}
}

// TestAgent_InjectRecalledMemories_ScoreThreshold covers the ScoreThreshold
// filtering branch and the all-dropped suppression branch of the injection seam.
func TestAgent_InjectRecalledMemories_ScoreThreshold(t *testing.T) {
	a := &Agent{MemoryRecall: RecallConfig{ScoreThreshold: 0.5}}

	// Mixed scores: the strong one is kept, the weak one dropped.
	msgs := a.injectRecalledMemories([]memory.RecalledMemory{
		{Key: "strong", Content: "strong: keep me", Score: 0.9},
		{Key: "weak", Content: "weak: drop me", Score: 0.2},
	})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 system message, got %d", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "strong: keep me") {
		t.Errorf("above-threshold memory missing: %q", msgs[0].Content)
	}
	if strings.Contains(msgs[0].Content, "weak: drop me") {
		t.Errorf("below-threshold memory not dropped: %q", msgs[0].Content)
	}

	// When every candidate is below threshold, nothing is injected.
	if got := a.injectRecalledMemories([]memory.RecalledMemory{
		{Key: "weak", Content: "weak", Score: 0.1},
	}); got != nil {
		t.Errorf("all-dropped recall should inject nil, got %v", got)
	}
}

// TestAgent_Recall_UserIsolation verifies the recall path threads the agent's
// UserID scope, so an agent recalls only its user's memories.
func TestAgent_Recall_UserIsolation(t *testing.T) {
	store := newTestStorage()
	vstore := newRecallVectorStore()

	// Two tenants seed equally-relevant memories in the same agent collection.
	aliceMgr := memory.NewManager("a1", "alice", memory.NewStore("a1", store), &testProvider{response: &model.ChatResponse{Content: "[]"}}).
		WithVectorIndex(constEmbedder{}, vstore, "m", 4)
	bobMgr := memory.NewManager("a1", "bob", memory.NewStore("a1", store), &testProvider{response: &model.ChatResponse{Content: "[]"}}).
		WithVectorIndex(constEmbedder{}, vstore, "m", 4)
	rememberFact(t, aliceMgr, "favorite_color", "blue")
	rememberFact(t, bobMgr, "favorite_color", "red")

	// The agent shares one manager but is scoped to alice via WithUserID.
	a, err := New("a1", "Test").
		WithModel(&testProvider{response: &model.ChatResponse{Content: "hi"}}).
		WithMemoryManager(aliceMgr).
		WithUserID("alice").
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	req, _, err := a.buildChatRequest(context.Background(), "what is my favorite color?")
	if err != nil {
		t.Fatalf("buildChatRequest: %v", err)
	}
	sys := systemContents(req.Messages)
	if !strings.Contains(sys, "favorite_color: blue") {
		t.Errorf("alice's agent did not recall alice's memory:\n%s", sys)
	}
	if strings.Contains(sys, "favorite_color: red") {
		t.Errorf("alice's agent leaked bob's memory:\n%s", sys)
	}
}
