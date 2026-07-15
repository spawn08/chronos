package qdrant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/spawn08/chronos/storage"
)

func TestUpsertBatching(t *testing.T) {
	tests := []struct {
		name      string
		batch     int
		count     int
		wantReqs  int
		wantTotal int
	}{
		{"single batch", 256, 10, 1, 10},
		{"exact multiple", 2, 4, 2, 4},
		{"remainder", 2, 5, 3, 5},
		{"one per request", 1, 3, 3, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var (
				mu      sync.Mutex
				reqs    int
				total   int
				mutated bool
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Points []struct {
						ID      string         `json:"id"`
						Payload map[string]any `json:"payload"`
					} `json:"points"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				mu.Lock()
				reqs++
				total += len(body.Points)
				mu.Unlock()
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			s := New(srv.URL)
			s.upsertBatch = tt.batch

			embeddings := make([]storage.Embedding, tt.count)
			shared := map[string]any{"tenant": "t1"}
			for i := range embeddings {
				embeddings[i] = storage.Embedding{
					ID:       string(rune('a' + i)),
					Vector:   []float32{float32(i)},
					Metadata: shared,
					Content:  "doc",
				}
			}

			if err := s.Upsert(context.Background(), "col", embeddings); err != nil {
				t.Fatalf("Upsert: %v", err)
			}

			// Caller metadata must not have been mutated with _content.
			if _, ok := shared["_content"]; ok {
				mutated = true
			}
			if mutated {
				t.Error("Upsert mutated caller metadata map")
			}
			if reqs != tt.wantReqs {
				t.Errorf("requests = %d, want %d", reqs, tt.wantReqs)
			}
			if total != tt.wantTotal {
				t.Errorf("total points = %d, want %d", total, tt.wantTotal)
			}
		})
	}
}

func TestUpsertEmpty(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := New(srv.URL)
	if err := s.Upsert(context.Background(), "col", nil); err != nil {
		t.Fatalf("Upsert(nil): %v", err)
	}
	if called {
		t.Error("empty Upsert should not make a request")
	}
}
