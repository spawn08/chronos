package lancedb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spawn08/chronos/storage"
)

func TestNew(t *testing.T) {
	s := New("http://localhost:8080", "my-key", "my-db")
	if s == nil {
		t.Fatal("New returned nil")
	}
	if s.baseURL != "http://localhost:8080" {
		t.Errorf("baseURL = %q, want %q", s.baseURL, "http://localhost:8080")
	}
	if s.apiKey != "my-key" {
		t.Errorf("apiKey = %q, want %q", s.apiKey, "my-key")
	}
	if s.dbName != "my-db" {
		t.Errorf("dbName = %q, want %q", s.dbName, "my-db")
	}
}

func TestClose(t *testing.T) {
	s := New("http://localhost", "", "db")
	if err := s.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
}

func TestQuoteIDs(t *testing.T) {
	tests := []struct {
		name string
		ids  []string
		want string
	}{
		{"empty", []string{}, ""},
		{"single", []string{"abc"}, "'abc'"},
		{"multiple", []string{"a", "b", "c"}, "'a', 'b', 'c'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := quoteIDs(tt.ids)
			if got != tt.want {
				t.Errorf("quoteIDs(%v) = %q, want %q", tt.ids, got, tt.want)
			}
		})
	}
}

func TestCreateCollection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "test-col" {
			t.Errorf("name = %v, want test-col", body["name"])
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := New(srv.URL, "", "mydb")
	if err := s.CreateCollection(context.Background(), "test-col", 128); err != nil {
		t.Errorf("CreateCollection() error: %v", err)
	}
}

func TestCreateCollection_APIKey(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := New(srv.URL, "secret-key", "mydb")
	s.CreateCollection(context.Background(), "col", 64)
	if gotKey != "secret-key" {
		t.Errorf("x-api-key header = %q, want %q", gotKey, "secret-key")
	}
}

func TestUpsert(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		data, ok := body["data"]
		if !ok {
			t.Error("body missing 'data'")
		}
		_ = data
		if body["mode"] != "overwrite" {
			t.Errorf("mode = %v, want overwrite", body["mode"])
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := New(srv.URL, "", "db")
	embeddings := []storage.Embedding{
		{ID: "1", Vector: []float32{0.1, 0.2}, Content: "hello", Metadata: map[string]any{"k": "v"}},
	}
	if err := s.Upsert(context.Background(), "my-col", embeddings); err != nil {
		t.Errorf("Upsert() error: %v", err)
	}
}

func TestSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"results": []map[string]any{
				{
					"id":        "1",
					"vector":    []float32{0.1, 0.2},
					"content":   "hello",
					"metadata":  `{"key":"val"}`,
					"_distance": float32(0.1),
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := New(srv.URL, "", "db")
	results, err := s.Search(context.Background(), "my-col", []float32{0.1, 0.2}, 1)
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
	// Score = 1 - distance = 1 - 0.1 = 0.9
	wantScore := float32(1 - 0.1)
	if results[0].Score != wantScore {
		t.Errorf("results[0].Score = %v, want %v", results[0].Score, wantScore)
	}
	if results[0].Metadata["key"] != "val" {
		t.Errorf("metadata key = %v, want val", results[0].Metadata["key"])
	}
}

func TestSearch_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer srv.Close()

	s := New(srv.URL, "", "db")
	_, err := s.Search(context.Background(), "col", []float32{0.1}, 5)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		filter, ok := body["filter"]
		if !ok {
			t.Error("body missing 'filter'")
		}
		_ = filter
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := New(srv.URL, "", "db")
	if err := s.Delete(context.Background(), "col", []string{"1", "2"}); err != nil {
		t.Errorf("Delete() error: %v", err)
	}
}
