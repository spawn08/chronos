package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIEmbeddings_Embed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		inputs, ok := body["input"].([]any)
		if !ok || len(inputs) == 0 {
			t.Fatal("expected non-empty input array")
		}

		resp := openAIEmbeddingResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
				{Embedding: []float32{0.4, 0.5, 0.6}, Index: 1},
			},
			Usage: struct {
				PromptTokens int `json:"prompt_tokens"`
				TotalTokens  int `json:"total_tokens"`
			}{PromptTokens: 5, TotalTokens: 5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	emb := NewOpenAIEmbeddingsWithConfig(ProviderConfig{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Model:   "text-embedding-3-small",
	})

	resp, err := emb.Embed(context.Background(), &EmbeddingRequest{
		Input: []string{"hello", "world"},
	})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(resp.Embeddings) != 2 {
		t.Fatalf("expected 2 embeddings, got %d", len(resp.Embeddings))
	}
	if len(resp.Embeddings[0]) != 3 {
		t.Fatalf("expected 3 dimensions, got %d", len(resp.Embeddings[0]))
	}
	if resp.Usage.PromptTokens != 5 {
		t.Fatalf("expected 5 prompt tokens, got %d", resp.Usage.PromptTokens)
	}
}

func TestOpenAIEmbeddings_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"invalid key"}}`))
	}))
	defer srv.Close()

	emb := NewOpenAIEmbeddingsWithConfig(ProviderConfig{
		APIKey:  "bad-key",
		BaseURL: srv.URL,
	})

	_, err := emb.Embed(context.Background(), &EmbeddingRequest{
		Input: []string{"hello"},
	})
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}
