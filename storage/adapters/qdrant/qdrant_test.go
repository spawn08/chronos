package qdrant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spawn08/chronos/storage"
)

func TestNew(t *testing.T) {
	s := New("http://localhost:6333")
	if s == nil {
		t.Fatal("New returned nil")
	}
	if s.baseURL != "http://localhost:6333" {
		t.Errorf("baseURL = %q, want %q", s.baseURL, "http://localhost:6333")
	}
}

func TestClose(t *testing.T) {
	s := New("http://localhost:6333")
	if err := s.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
}

func TestCreateCollection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/collections/my-col" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		vecs, ok := body["vectors"]
		if !ok {
			t.Error("body missing 'vectors'")
		}
		_ = vecs
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":true}`))
	}))
	defer srv.Close()

	s := New(srv.URL)
	if err := s.CreateCollection(context.Background(), "my-col", 128); err != nil {
		t.Errorf("CreateCollection() error: %v", err)
	}
}

func TestCreateCollection_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"status":"error"}`))
	}))
	defer srv.Close()

	s := New(srv.URL)
	err := s.CreateCollection(context.Background(), "col", 128)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestUpsert(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		points, ok := body["points"]
		if !ok {
			t.Error("body missing 'points'")
		}
		_ = points
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":{"operation_id":1,"status":"completed"}}`))
	}))
	defer srv.Close()

	s := New(srv.URL)
	embeddings := []storage.Embedding{
		{ID: "1", Vector: []float32{0.1, 0.2}, Content: "hello", Metadata: map[string]any{"key": "val"}},
		{ID: "2", Vector: []float32{0.3, 0.4}, Content: "world"},
	}
	if err := s.Upsert(context.Background(), "my-col", embeddings); err != nil {
		t.Errorf("Upsert() error: %v", err)
	}
}

func TestSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		resp := map[string]any{
			"result": []map[string]any{
				{
					"id":      "1",
					"score":   0.95,
					"payload": map[string]any{"_content": "hello", "key": "val"},
				},
				{
					"id":      "2",
					"score":   0.80,
					"payload": map[string]any{"_content": "world"},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := New(srv.URL)
	results, err := s.Search(context.Background(), "my-col", []float32{0.1, 0.2}, 2)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "1" {
		t.Errorf("results[0].ID = %q, want %q", results[0].ID, "1")
	}
	if results[0].Content != "hello" {
		t.Errorf("results[0].Content = %q, want %q", results[0].Content, "hello")
	}
	if results[0].Score != 0.95 {
		t.Errorf("results[0].Score = %v, want 0.95", results[0].Score)
	}
	// _content should be stripped from metadata
	if _, ok := results[0].Metadata["_content"]; ok {
		t.Error("_content should be stripped from metadata")
	}
}

func TestSearch_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"server error"}`))
	}))
	defer srv.Close()

	s := New(srv.URL)
	_, err := s.Search(context.Background(), "col", []float32{0.1}, 5)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["points"]; !ok {
			t.Error("body missing 'points'")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":{"operation_id":2,"status":"completed"}}`))
	}))
	defer srv.Close()

	s := New(srv.URL)
	if err := s.Delete(context.Background(), "my-col", []string{"1", "2"}); err != nil {
		t.Errorf("Delete() error: %v", err)
	}
}
