package milvus

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spawn08/chronos/storage"
)

func TestNew(t *testing.T) {
	s := New("http://localhost:19530", "my-token")
	if s == nil {
		t.Fatal("New returned nil")
	}
	if s.endpoint != "http://localhost:19530" {
		t.Errorf("endpoint = %q", s.endpoint)
	}
	if s.token != "my-token" {
		t.Errorf("token = %q", s.token)
	}
}

func TestClose(t *testing.T) {
	s := New("http://localhost:19530", "")
	if err := s.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
}

func TestCreateCollection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/vectordb/collections/create" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["collectionName"] != "my-col" {
			t.Errorf("collectionName = %v", body["collectionName"])
		}
		if body["metricType"] != "COSINE" {
			t.Errorf("metricType = %v", body["metricType"])
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	s := New(srv.URL, "")
	if err := s.CreateCollection(context.Background(), "my-col", 128); err != nil {
		t.Errorf("CreateCollection() error: %v", err)
	}
}

func TestCreateCollection_WithToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	s := New(srv.URL, "secret-token")
	s.CreateCollection(context.Background(), "col", 64)
	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer secret-token")
	}
}

func TestCreateCollection_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"code":400,"message":"error"}`))
	}))
	defer srv.Close()

	s := New(srv.URL, "")
	err := s.CreateCollection(context.Background(), "col", 128)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestUpsert(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/vectordb/entities/upsert" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["collectionName"] != "my-col" {
			t.Errorf("collectionName = %v", body["collectionName"])
		}
		if _, ok := body["data"]; !ok {
			t.Error("body missing 'data'")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	s := New(srv.URL, "")
	embeddings := []storage.Embedding{
		{ID: "1", Vector: []float32{0.1, 0.2}, Content: "hello", Metadata: map[string]any{"k": "v"}},
	}
	if err := s.Upsert(context.Background(), "my-col", embeddings); err != nil {
		t.Errorf("Upsert() error: %v", err)
	}
}

func TestSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/vectordb/entities/search" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		resp := map[string]any{
			"data": []map[string]any{
				{
					"id":       "1",
					"distance": float32(0.05),
					"content":  "hello",
					"metadata": `{"key":"val"}`,
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := New(srv.URL, "")
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
	wantScore := float32(1 - 0.05)
	if results[0].Score != wantScore {
		t.Errorf("results[0].Score = %v, want %v", results[0].Score, wantScore)
	}
}

func TestSearch_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"code":500}`))
	}))
	defer srv.Close()

	s := New(srv.URL, "")
	_, err := s.Search(context.Background(), "col", []float32{0.1}, 5)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/vectordb/entities/delete" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["collectionName"] != "my-col" {
			t.Errorf("collectionName = %v", body["collectionName"])
		}
		if _, ok := body["ids"]; !ok {
			t.Error("body missing 'ids'")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	s := New(srv.URL, "")
	if err := s.Delete(context.Background(), "my-col", []string{"1", "2"}); err != nil {
		t.Errorf("Delete() error: %v", err)
	}
}
