package model

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// GoogleEmbeddings tests
// ---------------------------------------------------------------------------

func TestNewGoogleEmbeddings_Defaults(t *testing.T) {
	e := NewGoogleEmbeddings("key", "")
	if e.config.Model != "text-embedding-004" {
		t.Errorf("Model=%q", e.config.Model)
	}
}

func TestNewGoogleEmbeddingsWithConfig_Defaults(t *testing.T) {
	e := NewGoogleEmbeddingsWithConfig(ProviderConfig{APIKey: "key"})
	if e.config.BaseURL == "" {
		t.Error("expected default BaseURL")
	}
	if e.config.Model == "" {
		t.Error("expected default Model")
	}
}

func TestGoogleEmbeddings_Embed_Success(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"embeddings":[{"values":[0.1,0.2,0.3]},{"values":[0.4,0.5,0.6]}]}`)
	}))
	defer svr.Close()

	e := NewGoogleEmbeddingsWithConfig(ProviderConfig{BaseURL: svr.URL, APIKey: "key", Model: "text-embedding-004"})
	resp, err := e.Embed(context.Background(), &EmbeddingRequest{
		Input: []string{"hello", "world"},
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(resp.Embeddings) != 2 {
		t.Errorf("expected 2 embeddings, got %d", len(resp.Embeddings))
	}
}

func TestGoogleEmbeddings_Embed_HTTPError(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":"forbidden"}`)
	}))
	defer svr.Close()

	e := NewGoogleEmbeddingsWithConfig(ProviderConfig{BaseURL: svr.URL, APIKey: "key"})
	_, err := e.Embed(context.Background(), &EmbeddingRequest{Input: []string{"test"}})
	if err == nil {
		t.Fatal("expected error for 403")
	}
}

func TestGoogleEmbeddings_Embed_WithModel(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"embeddings":[{"values":[0.1]}]}`)
	}))
	defer svr.Close()

	e := NewGoogleEmbeddingsWithConfig(ProviderConfig{BaseURL: svr.URL, APIKey: "key"})
	resp, err := e.Embed(context.Background(), &EmbeddingRequest{
		Input: []string{"hello"},
		Model: "embedding-001",
	})
	if err != nil {
		t.Fatalf("Embed with model override: %v", err)
	}
	if len(resp.Embeddings) != 1 {
		t.Errorf("expected 1 embedding, got %d", len(resp.Embeddings))
	}
}

// ---------------------------------------------------------------------------
// OllamaEmbeddings tests
// ---------------------------------------------------------------------------

func TestNewOllamaEmbeddings_Defaults(t *testing.T) {
	e := NewOllamaEmbeddings("", "")
	if e.model != "nomic-embed-text" {
		t.Errorf("model=%q", e.model)
	}
}

func TestOllamaEmbeddings_Embed_Success(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"embedding":[0.1,0.2,0.3]}`)
	}))
	defer svr.Close()

	e := NewOllamaEmbeddings(svr.URL, "nomic-embed-text")
	resp, err := e.Embed(context.Background(), &EmbeddingRequest{
		Input: []string{"hello"},
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(resp.Embeddings) != 1 {
		t.Errorf("expected 1 embedding, got %d", len(resp.Embeddings))
	}
}

func TestOllamaEmbeddings_Embed_HTTPError(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"server error"}`)
	}))
	defer svr.Close()

	e := NewOllamaEmbeddings(svr.URL, "model")
	_, err := e.Embed(context.Background(), &EmbeddingRequest{Input: []string{"test"}})
	if err == nil {
		t.Fatal("expected error for 500")
	}
}

func TestOllamaEmbeddings_Embed_WithModelOverride(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"embedding":[0.5]}`)
	}))
	defer svr.Close()

	e := NewOllamaEmbeddings(svr.URL, "default-model")
	resp, err := e.Embed(context.Background(), &EmbeddingRequest{
		Input: []string{"test"},
		Model: "override-model",
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(resp.Embeddings) != 1 {
		t.Errorf("expected 1 embedding, got %d", len(resp.Embeddings))
	}
}

// ---------------------------------------------------------------------------
// OpenAIEmbeddings tests
// ---------------------------------------------------------------------------

func TestNewOpenAIEmbeddings_Defaults(t *testing.T) {
	e := NewOpenAIEmbeddings("key")
	if e.config.Model != "text-embedding-3-small" {
		t.Errorf("Model=%q", e.config.Model)
	}
}

func TestNewOpenAIEmbeddingsWithConfig_OrgID(t *testing.T) {
	e := NewOpenAIEmbeddingsWithConfig(ProviderConfig{
		APIKey: "key",
		OrgID:  "org-123",
	})
	if e.config.Model == "" {
		t.Error("expected default model")
	}
}

func TestOpenAIEmbeddings_Embed_Success(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"data":[{"embedding":[0.1,0.2],"index":0},{"embedding":[0.3,0.4],"index":1}],"usage":{"prompt_tokens":5,"total_tokens":5}}`)
	}))
	defer svr.Close()

	e := NewOpenAIEmbeddingsWithConfig(ProviderConfig{BaseURL: svr.URL, APIKey: "key", Model: "text-embedding-3-small"})
	resp, err := e.Embed(context.Background(), &EmbeddingRequest{
		Input: []string{"hello", "world"},
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(resp.Embeddings) != 2 {
		t.Errorf("expected 2 embeddings, got %d", len(resp.Embeddings))
	}
	if resp.Usage.PromptTokens != 5 {
		t.Errorf("expected 5 prompt tokens, got %d", resp.Usage.PromptTokens)
	}
}

func TestOpenAIEmbeddings_Embed_HTTPError(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"invalid key"}}`)
	}))
	defer svr.Close()

	e := NewOpenAIEmbeddingsWithConfig(ProviderConfig{BaseURL: svr.URL, APIKey: "bad"})
	_, err := e.Embed(context.Background(), &EmbeddingRequest{Input: []string{"test"}})
	if err == nil {
		t.Fatal("expected error for 401")
	}
}
