package chromadb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spawn08/chronos/storage"
)

// collectionResponse returns a JSON response for a ChromaDB collection.
func collectionResponse(id, name string) string {
	return `{"id":"` + id + `","name":"` + name + `"}`
}

func TestNew(t *testing.T) {
	s := New("http://localhost:8000")
	if s == nil {
		t.Fatal("New returned nil")
	}
	if s.baseURL != "http://localhost:8000" {
		t.Errorf("baseURL = %q, want %q", s.baseURL, "http://localhost:8000")
	}
	if s.tenant != "default_tenant" {
		t.Errorf("tenant = %q, want %q", s.tenant, "default_tenant")
	}
	if s.db != "default_database" {
		t.Errorf("db = %q, want %q", s.db, "default_database")
	}
}

func TestWithTenant(t *testing.T) {
	s := New("http://localhost:8000").WithTenant("my_tenant")
	if s.tenant != "my_tenant" {
		t.Errorf("tenant = %q, want %q", s.tenant, "my_tenant")
	}
}

func TestWithDatabase(t *testing.T) {
	s := New("http://localhost:8000").WithDatabase("my_db")
	if s.db != "my_db" {
		t.Errorf("db = %q, want %q", s.db, "my_db")
	}
}

func TestClose(t *testing.T) {
	s := New("http://localhost:8000")
	if err := s.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

func TestCreateCollection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"col-1","name":"test"}`))
	}))
	defer srv.Close()

	s := New(srv.URL)
	ctx := context.Background()
	if err := s.CreateCollection(ctx, "test", 128); err != nil {
		t.Errorf("CreateCollection() error: %v", err)
	}
}

func TestCreateCollection_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal error"}`))
	}))
	defer srv.Close()

	s := New(srv.URL)
	ctx := context.Background()
	err := s.CreateCollection(ctx, "test", 128)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestUpsert(t *testing.T) {
	const collectionID = "col-abc"
	mux := http.NewServeMux()

	// Handle collection lookup (GET)
	mux.HandleFunc("/api/v1/tenants/default_tenant/databases/default_database/collections/my-col", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"` + collectionID + `","name":"my-col"}`))
	})

	// Handle upsert (POST)
	mux.HandleFunc("/api/v1/collections/"+collectionID+"/upsert", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["ids"]; !ok {
			t.Error("upsert body missing 'ids'")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	s := New(srv.URL)
	ctx := context.Background()
	embeddings := []storage.Embedding{
		{ID: "e1", Vector: []float32{0.1, 0.2}, Content: "hello", Metadata: map[string]any{"k": "v"}},
		{ID: "e2", Vector: []float32{0.3, 0.4}, Content: "world"},
	}
	if err := s.Upsert(ctx, "my-col", embeddings); err != nil {
		t.Errorf("Upsert() error: %v", err)
	}
}

func TestSearch(t *testing.T) {
	const collectionID = "col-abc"
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/tenants/default_tenant/databases/default_database/collections/my-col", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"` + collectionID + `","name":"my-col"}`))
	})

	mux.HandleFunc("/api/v1/collections/"+collectionID+"/query", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		resp := map[string]any{
			"ids":        [][]string{{"e1", "e2"}},
			"embeddings": [][][]float32{{{0.1, 0.2}, {0.3, 0.4}}},
			"metadatas":  [][]map[string]any{{{"k": "v"}, {}}},
			"documents":  [][]string{{"hello", "world"}},
			"distances":  [][]float32{{0.1, 0.3}},
		}
		json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	s := New(srv.URL)
	ctx := context.Background()
	results, err := s.Search(ctx, "my-col", []float32{0.1, 0.2}, 2)
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "e1" {
		t.Errorf("results[0].ID = %q, want %q", results[0].ID, "e1")
	}
	// Score should be 1 - distance
	wantScore := float32(1 - 0.1)
	if results[0].Score != wantScore {
		t.Errorf("results[0].Score = %v, want %v", results[0].Score, wantScore)
	}
}

func TestSearch_Empty(t *testing.T) {
	const collectionID = "col-abc"
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/tenants/default_tenant/databases/default_database/collections/my-col", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"` + collectionID + `","name":"my-col"}`))
	})

	mux.HandleFunc("/api/v1/collections/"+collectionID+"/query", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		resp := map[string]any{
			"ids":       [][]string{{}},
			"metadatas": [][]map[string]any{{}},
			"documents": [][]string{{}},
			"distances": [][]float32{{}},
		}
		json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	s := New(srv.URL)
	results, err := s.Search(context.Background(), "my-col", []float32{0.1}, 5)
	if err != nil {
		t.Errorf("Search() error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for empty response, got %v", results)
	}
}

func TestDelete(t *testing.T) {
	const collectionID = "col-abc"
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/tenants/default_tenant/databases/default_database/collections/my-col", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"` + collectionID + `","name":"my-col"}`))
	})

	mux.HandleFunc("/api/v1/collections/"+collectionID+"/delete", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		ids, ok := body["ids"]
		if !ok {
			t.Error("delete body missing 'ids'")
		}
		_ = ids
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	s := New(srv.URL)
	if err := s.Delete(context.Background(), "my-col", []string{"e1", "e2"}); err != nil {
		t.Errorf("Delete() error: %v", err)
	}
}

func TestGetCollectionID_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	s := New(srv.URL)
	_, err := s.getCollectionID(context.Background(), "missing-col")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}
