package chromadb

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spawn08/chronos/storage"
)

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestGetCollectionID_DecodeError_ITER6(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	s := New(srv.URL)
	_, err := s.getCollectionID(context.Background(), "col")
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestSearch_ResponseDecodeError_ITER6(t *testing.T) {
	const collectionID = "col-abc"
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tenants/default_tenant/databases/default_database/collections/my-col", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"` + collectionID + `","name":"my-col"}`))
	})
	mux.HandleFunc("/api/v1/collections/"+collectionID+"/query", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s := New(srv.URL)
	_, err := s.Search(context.Background(), "my-col", []float32{0.1}, 2)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("expected search decode error, got %v", err)
	}
}

func TestPost_ClientDoError_ITER6(t *testing.T) {
	s := New("http://example.invalid")
	s.client = &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("network down")
	})}
	_, err := s.post(context.Background(), "/api/v1/x", map[string]any{"a": 1})
	if err == nil {
		t.Fatal("expected post error")
	}
}

func TestUpsert_GetCollectionError_ITER6(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	s := New(srv.URL)
	err := s.Upsert(context.Background(), "missing", []storage.Embedding{{ID: "e1", Vector: []float32{1}}})
	if err == nil || !strings.Contains(err.Error(), "chromadb upsert") {
		t.Fatalf("expected upsert error, got %v", err)
	}
}

func TestDelete_PostHTTPError_ITER6(t *testing.T) {
	const collectionID = "col-del"
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tenants/default_tenant/databases/default_database/collections/del-col", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"` + collectionID + `","name":"del-col"}`))
	})
	mux.HandleFunc("/api/v1/collections/"+collectionID+"/delete", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `fail`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	s := New(srv.URL)
	err := s.Delete(context.Background(), "del-col", []string{"e1"})
	if err == nil {
		t.Fatal("expected delete error")
	}
}
