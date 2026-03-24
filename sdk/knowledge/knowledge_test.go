package knowledge

import (
	"context"
	"errors"
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/storage"
)

// ---- Stub implementations ----

type stubVectorStore struct {
	upserted   []storage.Embedding
	results    []storage.SearchResult
	upsertErr  error
	searchErr  error
	createErr  error
	collection string
	dimension  int
}

func (s *stubVectorStore) CreateCollection(_ context.Context, name string, dim int) error {
	s.collection = name
	s.dimension = dim
	return s.createErr
}
func (s *stubVectorStore) Upsert(_ context.Context, _ string, embeddings []storage.Embedding) error {
	s.upserted = append(s.upserted, embeddings...)
	return s.upsertErr
}
func (s *stubVectorStore) Search(_ context.Context, _ string, _ []float32, _ int) ([]storage.SearchResult, error) {
	return s.results, s.searchErr
}
func (s *stubVectorStore) Delete(_ context.Context, _ string, _ []string) error { return nil }
func (s *stubVectorStore) Close() error                                          { return nil }

type stubEmbedder struct {
	embeddings [][]float32
	err        error
}

func (e *stubEmbedder) Embed(_ context.Context, req *model.EmbeddingRequest) (*model.EmbeddingResponse, error) {
	if e.err != nil {
		return nil, e.err
	}
	embs := e.embeddings
	if len(embs) == 0 {
		embs = make([][]float32, len(req.Input))
		for i := range embs {
			embs[i] = []float32{0.1, 0.2, 0.3}
		}
	}
	return &model.EmbeddingResponse{Embeddings: embs}, nil
}

// ---- Document tests ----

func TestDocumentFields(t *testing.T) {
	doc := Document{
		ID:       "doc-1",
		Content:  "hello world",
		Metadata: map[string]any{"source": "test"},
		Score:    0.95,
	}
	if doc.ID != "doc-1" {
		t.Errorf("got ID %q", doc.ID)
	}
	if doc.Score != 0.95 {
		t.Errorf("got Score %v", doc.Score)
	}
}

// ---- VectorKnowledge tests ----

func TestNewVectorKnowledge(t *testing.T) {
	vs := &stubVectorStore{}
	emb := &stubEmbedder{}
	vk := NewVectorKnowledge("col", 3, vs, emb, "text-embedding-3-small")

	if vk.Collection != "col" {
		t.Errorf("Collection: got %q", vk.Collection)
	}
	if vk.Dimension != 3 {
		t.Errorf("Dimension: got %d", vk.Dimension)
	}
	if vk.EmbedModel != "text-embedding-3-small" {
		t.Errorf("EmbedModel: got %q", vk.EmbedModel)
	}
}

func TestAddDocuments(t *testing.T) {
	vs := &stubVectorStore{}
	emb := &stubEmbedder{}
	vk := NewVectorKnowledge("col", 3, vs, emb, "model")

	vk.AddDocuments(Document{ID: "a", Content: "foo"}, Document{ID: "b", Content: "bar"})
	if len(vk.documents) != 2 {
		t.Errorf("expected 2 documents, got %d", len(vk.documents))
	}
}

func TestLoadNoDocuments(t *testing.T) {
	vs := &stubVectorStore{}
	emb := &stubEmbedder{}
	vk := NewVectorKnowledge("col", 3, vs, emb, "model")

	if err := vk.Load(context.Background()); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if vs.collection != "col" {
		t.Errorf("expected CreateCollection to be called with 'col'")
	}
}

func TestLoadWithDocuments(t *testing.T) {
	vs := &stubVectorStore{}
	emb := &stubEmbedder{}
	vk := NewVectorKnowledge("col", 3, vs, emb, "model")

	vk.AddDocuments(
		Document{ID: "d1", Content: "alpha"},
		Document{Content: "beta"}, // no ID — should auto-generate
	)

	if err := vk.Load(context.Background()); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(vs.upserted) != 2 {
		t.Errorf("expected 2 upserted, got %d", len(vs.upserted))
	}
	if vs.upserted[0].ID != "d1" {
		t.Errorf("first doc ID: got %q", vs.upserted[0].ID)
	}
	if vs.upserted[1].ID == "" {
		t.Error("expected auto-generated ID for doc without ID")
	}
}

func TestLoadCreateCollectionError(t *testing.T) {
	vs := &stubVectorStore{createErr: errors.New("create failed")}
	emb := &stubEmbedder{}
	vk := NewVectorKnowledge("col", 3, vs, emb, "model")

	err := vk.Load(context.Background())
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestLoadEmbedError(t *testing.T) {
	vs := &stubVectorStore{}
	emb := &stubEmbedder{err: errors.New("embed failed")}
	vk := NewVectorKnowledge("col", 3, vs, emb, "model")
	vk.AddDocuments(Document{ID: "x", Content: "test"})

	err := vk.Load(context.Background())
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestSearch(t *testing.T) {
	vs := &stubVectorStore{
		results: []storage.SearchResult{
			{Embedding: storage.Embedding{ID: "r1", Content: "result", Metadata: map[string]any{"k": "v"}}, Score: 0.8},
		},
	}
	emb := &stubEmbedder{}
	vk := NewVectorKnowledge("col", 3, vs, emb, "model")

	docs, err := vk.Search(context.Background(), "query", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].ID != "r1" {
		t.Errorf("doc ID: got %q", docs[0].ID)
	}
	if docs[0].Score != 0.8 {
		t.Errorf("doc Score: got %v", docs[0].Score)
	}
}

func TestSearchEmbedError(t *testing.T) {
	vs := &stubVectorStore{}
	emb := &stubEmbedder{err: errors.New("embed error")}
	vk := NewVectorKnowledge("col", 3, vs, emb, "model")

	_, err := vk.Search(context.Background(), "q", 3)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestSearchStoreError(t *testing.T) {
	vs := &stubVectorStore{searchErr: errors.New("search failed")}
	emb := &stubEmbedder{}
	vk := NewVectorKnowledge("col", 3, vs, emb, "model")

	_, err := vk.Search(context.Background(), "q", 3)
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestClose(t *testing.T) {
	vs := &stubVectorStore{}
	emb := &stubEmbedder{}
	vk := NewVectorKnowledge("col", 3, vs, emb, "model")
	if err := vk.Close(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
