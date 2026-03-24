package weaviate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spawn08/chronos/storage"
)

func TestNew(t *testing.T) {
	s := New("http://localhost:8080", "api-key")
	if s == nil {
		t.Fatal("New returned nil")
	}
	if s.endpoint != "http://localhost:8080" {
		t.Errorf("endpoint = %q", s.endpoint)
	}
	if s.apiKey != "api-key" {
		t.Errorf("apiKey = %q", s.apiKey)
	}
}

func TestClose(t *testing.T) {
	s := New("http://localhost:8080", "")
	if err := s.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
}

func TestCreateCollection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/schema" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["class"] != "MyCollection" {
			t.Errorf("class = %v, want MyCollection", body["class"])
		}
		if body["vectorizer"] != "none" {
			t.Errorf("vectorizer = %v, want none", body["vectorizer"])
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := New(srv.URL, "")
	if err := s.CreateCollection(context.Background(), "MyCollection", 128); err != nil {
		t.Errorf("CreateCollection() error: %v", err)
	}
}

func TestCreateCollection_WithAPIKey(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := New(srv.URL, "my-secret-key")
	s.CreateCollection(context.Background(), "Col", 64)
	if gotAuth != "Bearer my-secret-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer my-secret-key")
	}
}

func TestCreateCollection_NoAPIKey(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := New(srv.URL, "")
	s.CreateCollection(context.Background(), "Col", 64)
	if gotAuth != "" {
		t.Errorf("expected no Authorization header, got %q", gotAuth)
	}
}

func TestUpsert(t *testing.T) {
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := New(srv.URL, "")
	embeddings := []storage.Embedding{
		{ID: "e1", Vector: []float32{0.1, 0.2}, Content: "hello", Metadata: map[string]any{"k": "v"}},
		{ID: "e2", Vector: []float32{0.3, 0.4}, Content: "world"},
	}
	if err := s.Upsert(context.Background(), "MyCol", embeddings); err != nil {
		t.Errorf("Upsert() error: %v", err)
	}
	// One POST per embedding
	if len(requests) != 2 {
		t.Errorf("expected 2 requests, got %d", len(requests))
	}
}

func TestUpsert_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"error":"invalid vector"}`))
	}))
	defer srv.Close()

	s := New(srv.URL, "")
	embeddings := []storage.Embedding{
		{ID: "e1", Vector: []float32{0.1}, Content: "hello"},
	}
	err := s.Upsert(context.Background(), "Col", embeddings)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/graphql" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := map[string]any{
			"data": map[string]any{
				"Get": map[string]any{
					"MyCol": []map[string]any{
						{
							"content": "hello",
							"meta":    `{"key":"val"}`,
							"_additional": map[string]any{
								"id":       "e1",
								"distance": float32(0.05),
							},
						},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := New(srv.URL, "")
	results, err := s.Search(context.Background(), "MyCol", []float32{0.1, 0.2}, 1)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "e1" {
		t.Errorf("results[0].ID = %q, want %q", results[0].ID, "e1")
	}
	if results[0].Content != "hello" {
		t.Errorf("results[0].Content = %q, want %q", results[0].Content, "hello")
	}
	wantScore := float32(1 - 0.05)
	if results[0].Score != wantScore {
		t.Errorf("results[0].Score = %v, want %v", results[0].Score, wantScore)
	}
}

func TestDelete(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := New(srv.URL, "")
	if err := s.Delete(context.Background(), "MyCol", []string{"e1", "e2"}); err != nil {
		t.Errorf("Delete() error: %v", err)
	}
	if len(paths) != 2 {
		t.Errorf("expected 2 DELETE requests, got %d", len(paths))
	}
}
