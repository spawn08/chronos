// Package pinecone provides a Pinecone-backed VectorStore adapter for Chronos.
package pinecone

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/chronos-ai/chronos/storage"
)

// Store implements storage.VectorStore using Pinecone's REST API.
type Store struct {
	host   string
	apiKey string
	client *http.Client
}

// New creates a Pinecone vector store.
// host is the index endpoint, e.g. "https://my-index-abc123.svc.pinecone.io".
func New(host, apiKey string) *Store {
	return &Store{
		host:   host,
		apiKey: apiKey,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *Store) doJSON(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.host+path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("pinecone: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Api-Key", s.apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pinecone %s: %w", path, err)
	}
	defer resp.Body.Close()

	var result json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("pinecone %s: status %d: %s", path, resp.StatusCode, string(result))
	}
	return result, nil
}

func (s *Store) CreateCollection(_ context.Context, _ string, _ int) error {
	return nil
}

func (s *Store) Upsert(ctx context.Context, _ string, embeddings []storage.Embedding) error {
	vectors := make([]map[string]any, len(embeddings))
	for i, e := range embeddings {
		meta := e.Metadata
		if meta == nil {
			meta = map[string]any{}
		}
		meta["_content"] = e.Content
		vectors[i] = map[string]any{
			"id":       e.ID,
			"values":   e.Vector,
			"metadata": meta,
		}
	}
	_, err := s.doJSON(ctx, http.MethodPost, "/vectors/upsert", map[string]any{"vectors": vectors})
	return err
}

func (s *Store) Search(ctx context.Context, _ string, query []float32, topK int) ([]storage.SearchResult, error) {
	raw, err := s.doJSON(ctx, http.MethodPost, "/query", map[string]any{
		"vector":          query,
		"topK":            topK,
		"includeMetadata": true,
	})
	if err != nil {
		return nil, err
	}

	var resp struct {
		Matches []struct {
			ID       string         `json:"id"`
			Score    float32        `json:"score"`
			Metadata map[string]any `json:"metadata"`
		} `json:"matches"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("pinecone search decode: %w", err)
	}

	results := make([]storage.SearchResult, len(resp.Matches))
	for i, m := range resp.Matches {
		content, _ := m.Metadata["_content"].(string)
		delete(m.Metadata, "_content")
		results[i] = storage.SearchResult{
			Embedding: storage.Embedding{
				ID:       m.ID,
				Metadata: m.Metadata,
				Content:  content,
			},
			Score: m.Score,
		}
	}
	return results, nil
}

func (s *Store) Delete(ctx context.Context, _ string, ids []string) error {
	_, err := s.doJSON(ctx, http.MethodPost, "/vectors/delete", map[string]any{"ids": ids})
	return err
}

func (s *Store) Close() error { return nil }
