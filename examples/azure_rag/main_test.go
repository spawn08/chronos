package main

import (
	"context"
	"testing"

	"github.com/spawn08/chronos/storage"
)

// These tests are fully offline: they exercise the in-memory VectorStore and
// the cosine-similarity ranking with tiny deterministic vectors. No Azure or
// network access is involved.

func TestMemoryVectorStoreSearchOrdering(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryVectorStore()
	defer store.Close()

	if err := store.CreateCollection(ctx, "docs", 3); err != nil {
		t.Fatalf("create collection: %v", err)
	}

	// Three orthogonal-ish vectors; the query aligns most with "x".
	err := store.Upsert(ctx, "docs", []storage.Embedding{
		{ID: "x", Vector: []float32{1, 0, 0}, Content: "x-axis"},
		{ID: "y", Vector: []float32{0, 1, 0}, Content: "y-axis"},
		{ID: "xy", Vector: []float32{1, 1, 0}, Content: "xy-diagonal"},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	query := []float32{1, 0, 0}
	results, err := store.Search(ctx, "docs", query, 3)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Most similar to the x-axis query must be "x", then "xy", then "y".
	wantOrder := []string{"x", "xy", "y"}
	for i, want := range wantOrder {
		if results[i].ID != want {
			t.Errorf("result[%d].ID = %q, want %q (scores: %v)", i, results[i].ID, want, scores(results))
		}
	}

	// Scores must be monotonically non-increasing.
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("scores not sorted descending: %v", scores(results))
		}
	}
}

func TestMemoryVectorStoreTopK(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryVectorStore()
	_ = store.Upsert(ctx, "docs", []storage.Embedding{
		{ID: "a", Vector: []float32{1, 0}},
		{ID: "b", Vector: []float32{0, 1}},
	})
	results, err := store.Search(ctx, "docs", []float32{1, 0}, 1)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 || results[0].ID != "a" {
		t.Errorf("topK=1 = %v, want single result 'a'", scores(results))
	}
}

func TestMemoryVectorStoreUpsertReplaceAndDelete(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryVectorStore()

	_ = store.Upsert(ctx, "docs", []storage.Embedding{{ID: "a", Vector: []float32{1, 0}, Content: "v1"}})
	_ = store.Upsert(ctx, "docs", []storage.Embedding{{ID: "a", Vector: []float32{1, 0}, Content: "v2"}})

	results, _ := store.Search(ctx, "docs", []float32{1, 0}, 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 embedding after replace, got %d", len(results))
	}
	if results[0].Content != "v2" {
		t.Errorf("content = %q, want %q (upsert should replace by ID)", results[0].Content, "v2")
	}

	if err := store.Delete(ctx, "docs", []string{"a"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	results, _ = store.Search(ctx, "docs", []float32{1, 0}, 10)
	if len(results) != 0 {
		t.Errorf("expected 0 embeddings after delete, got %d", len(results))
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float32
	}{
		{name: "identical", a: []float32{1, 0}, b: []float32{1, 0}, want: 1},
		{name: "orthogonal", a: []float32{1, 0}, b: []float32{0, 1}, want: 0},
		{name: "opposite", a: []float32{1, 0}, b: []float32{-1, 0}, want: -1},
		{name: "mismatched length", a: []float32{1, 0}, b: []float32{1}, want: 0},
		{name: "zero vector", a: []float32{0, 0}, b: []float32{1, 0}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			if diff := got - tt.want; diff > 1e-6 || diff < -1e-6 {
				t.Errorf("cosineSimilarity(%v,%v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func scores(results []storage.SearchResult) map[string]float32 {
	out := make(map[string]float32, len(results))
	for _, r := range results {
		out[r.ID] = r.Score
	}
	return out
}
