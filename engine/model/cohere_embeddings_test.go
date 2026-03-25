package model

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewCohereEmbeddings_Defaults(t *testing.T) {
	e := NewCohereEmbeddings("key", "")
	if e == nil {
		t.Fatal("expected non-nil")
	}
	if e.config.Model == "" {
		t.Error("expected default model")
	}
}

func TestNewCohereEmbeddingsWithConfig_Defaults(t *testing.T) {
	e := NewCohereEmbeddingsWithConfig(ProviderConfig{APIKey: "key"})
	if e.config.BaseURL == "" {
		t.Error("expected default BaseURL")
	}
	if e.config.Model == "" {
		t.Error("expected default Model")
	}
}

func TestCohereEmbeddings_Embed_Success(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"embeddings":[[0.1,0.2,0.3],[0.4,0.5,0.6]],"meta":{"billed_units":{"input_tokens":10}}}`)
	}))
	defer svr.Close()

	e := NewCohereEmbeddingsWithConfig(ProviderConfig{
		BaseURL: svr.URL,
		APIKey:  "key",
		Model:   "embed-english-v3.0",
	})

	resp, err := e.Embed(context.Background(), &EmbeddingRequest{
		Input: []string{"hello", "world"},
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(resp.Embeddings) != 2 {
		t.Errorf("expected 2 embeddings, got %d", len(resp.Embeddings))
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("expected 10 prompt tokens, got %d", resp.Usage.PromptTokens)
	}
}

func TestCohereEmbeddings_Embed_HTTPError(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"invalid api key"}`)
	}))
	defer svr.Close()

	e := NewCohereEmbeddingsWithConfig(ProviderConfig{BaseURL: svr.URL, APIKey: "bad"})
	_, err := e.Embed(context.Background(), &EmbeddingRequest{Input: []string{"test"}})
	if err == nil {
		t.Fatal("expected error for 401")
	}
}

func TestCohereEmbeddings_Embed_WithModel(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"embeddings":[[0.1]],"meta":{"billed_units":{"input_tokens":1}}}`)
	}))
	defer svr.Close()

	e := NewCohereEmbeddingsWithConfig(ProviderConfig{BaseURL: svr.URL, APIKey: "key"})
	// Override model in request
	resp, err := e.Embed(context.Background(), &EmbeddingRequest{
		Input: []string{"test"},
		Model: "embed-multilingual-v3.0",
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(resp.Embeddings) != 1 {
		t.Errorf("expected 1 embedding, got %d", len(resp.Embeddings))
	}
}
