package main

import (
	"context"
	"testing"

	"github.com/spawn08/chronos/sdk/knowledge"
)

// TestRetrievalRanksMostRelevantFirst verifies the offline RAG pipeline returns
// the passage most related to the query as the top hit — no network required.
func TestRetrievalRanksMostRelevantFirst(t *testing.T) {
	ctx := context.Background()

	kb := knowledge.NewVectorKnowledge(
		"test-docs",
		embedDimension,
		newMemoryVectorStore(),
		newHashEmbedder(embedDimension),
		"hash-stub",
		knowledge.WithTopK(3),
	)

	kb.AddDocuments(
		knowledge.Document{ID: "goroutines", Content: "Goroutines are lightweight threads managed by the Go runtime started with the go keyword."},
		knowledge.Document{ID: "channels", Content: "Channels are typed conduits that let goroutines send and receive values with synchronization."},
		knowledge.Document{ID: "interfaces", Content: "Interfaces in Go are satisfied implicitly by any type implementing the required methods."},
		knowledge.Document{ID: "context", Content: "The context package carries deadlines and cancellation signals across API boundaries."},
	)

	if err := kb.Load(ctx); err != nil {
		t.Fatalf("load: %v", err)
	}

	tests := []struct {
		name    string
		query   string
		wantTop string
	}{
		{name: "threads query hits goroutines", query: "lightweight threads runtime", wantTop: "goroutines"},
		{name: "cancellation query hits context", query: "deadlines cancellation across API boundaries", wantTop: "context"},
		{name: "methods query hits interfaces", query: "type implementing required methods", wantTop: "interfaces"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			docs, err := kb.Search(ctx, tt.query, 3)
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			if len(docs) == 0 {
				t.Fatal("expected at least one result")
			}
			if docs[0].ID != tt.wantTop {
				t.Errorf("top result = %q, want %q (scores: %+v)", docs[0].ID, tt.wantTop, docs)
			}
		})
	}
}
