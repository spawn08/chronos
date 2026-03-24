package pinecone

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spawn08/chronos/storage"
)

func TestNew(t *testing.T) {
	s := New("https://my-index.svc.pinecone.io", "api-key")
	if s == nil {
		t.Fatal("New returned nil")
	}
	if s.host != "https://my-index.svc.pinecone.io" {
		t.Errorf("host = %q", s.host)
	}
	if s.apiKey != "api-key" {
		t.Errorf("apiKey = %q", s.apiKey)
	}
}

func TestClose(t *testing.T) {
	s := New("https://host", "key")
	if err := s.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
}

func TestCreateCollection(t *testing.T) {
	s := New("https://host", "key")
	// CreateCollection is a no-op for Pinecone (index created externally)
	if err := s.CreateCollection(context.Background(), "my-index", 128); err != nil {
		t.Errorf("CreateCollection() error: %v", err)
	}
}

func TestUpsert(t *testing.T) {
	var gotAPIKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("Api-Key")
		if r.URL.Path != "/vectors/upsert" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		vectors, ok := body["vectors"]
		if !ok {
			t.Error("body missing 'vectors'")
		}
		_ = vectors
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"upsertedCount":2}`))
	}))
	defer srv.Close()

	s := New(srv.URL, "my-api-key")
	embeddings := []storage.Embedding{
		{ID: "1", Vector: []float32{0.1, 0.2}, Content: "hello", Metadata: map[string]any{"k": "v"}},
		{ID: "2", Vector: []float32{0.3, 0.4}, Content: "world"},
	}
	if err := s.Upsert(context.Background(), "my-index", embeddings); err != nil {
		t.Errorf("Upsert() error: %v", err)
	}
	if gotAPIKey != "my-api-key" {
		t.Errorf("Api-Key header = %q, want %q", gotAPIKey, "my-api-key")
	}
}

func TestSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/query" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := map[string]any{
			"matches": []map[string]any{
				{
					"id":    "1",
					"score": float32(0.95),
					"metadata": map[string]any{
						"_content": "hello",
						"key":      "val",
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := New(srv.URL, "key")
	results, err := s.Search(context.Background(), "my-index", []float32{0.1, 0.2}, 1)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "1" {
		t.Errorf("results[0].ID = %q, want %q", results[0].ID, "1")
	}
	if results[0].Content != "hello" {
		t.Errorf("results[0].Content = %q, want %q", results[0].Content, "hello")
	}
	// _content stripped from metadata
	if _, ok := results[0].Metadata["_content"]; ok {
		t.Error("_content should be stripped from metadata")
	}
	if results[0].Metadata["key"] != "val" {
		t.Errorf("metadata key = %v, want val", results[0].Metadata["key"])
	}
}

func TestSearch_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"invalid api key"}`))
	}))
	defer srv.Close()

	s := New(srv.URL, "bad-key")
	_, err := s.Search(context.Background(), "col", []float32{0.1}, 5)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vectors/delete" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["ids"]; !ok {
			t.Error("body missing 'ids'")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := New(srv.URL, "key")
	if err := s.Delete(context.Background(), "col", []string{"1", "2"}); err != nil {
		t.Errorf("Delete() error: %v", err)
	}
}
