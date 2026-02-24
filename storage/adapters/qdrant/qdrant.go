// Package qdrant provides a Qdrant-backed VectorStore adapter for Chronos.
package qdrant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/spawn08/chronos/storage"
)

// Store implements storage.VectorStore using Qdrant's REST API.
type Store struct {
	baseURL string
	client  *http.Client
}

// New creates a Qdrant vector store client.
func New(baseURL string) *Store {
	return &Store{
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

func (s *Store) CreateCollection(ctx context.Context, name string, dimension int) error {
	body := map[string]any{
		"vectors": map[string]any{
			"size":     dimension,
			"distance": "Cosine",
		},
	}
	return s.put(ctx, fmt.Sprintf("/collections/%s", name), body)
}

func (s *Store) Upsert(ctx context.Context, collection string, embeddings []storage.Embedding) error {
	points := make([]map[string]any, len(embeddings))
	for i, e := range embeddings {
		payload := e.Metadata
		if payload == nil {
			payload = map[string]any{}
		}
		payload["_content"] = e.Content
		points[i] = map[string]any{
			"id":      e.ID,
			"vector":  e.Vector,
			"payload": payload,
		}
	}
	body := map[string]any{"points": points}
	return s.put(ctx, fmt.Sprintf("/collections/%s/points", collection), body)
}

func (s *Store) Search(ctx context.Context, collection string, query []float32, topK int) ([]storage.SearchResult, error) {
	body := map[string]any{
		"vector":       query,
		"limit":        topK,
		"with_payload": true,
	}
	data, err := s.post(ctx, fmt.Sprintf("/collections/%s/points/search", collection), body)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Result []struct {
			ID      string         `json:"id"`
			Score   float32        `json:"score"`
			Payload map[string]any `json:"payload"`
			Vector  []float32      `json:"vector"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("qdrant search decode: %w", err)
	}

	results := make([]storage.SearchResult, len(resp.Result))
	for i, r := range resp.Result {
		content, _ := r.Payload["_content"].(string)
		delete(r.Payload, "_content")
		results[i] = storage.SearchResult{
			Embedding: storage.Embedding{
				ID:       r.ID,
				Vector:   r.Vector,
				Metadata: r.Payload,
				Content:  content,
			},
			Score: r.Score,
		}
	}
	return results, nil
}

func (s *Store) Delete(ctx context.Context, collection string, ids []string) error {
	body := map[string]any{"points": ids}
	return s.put(ctx, fmt.Sprintf("/collections/%s/points/delete", collection), body)
}

func (s *Store) Close() error { return nil }

// --- HTTP helpers ---

func (s *Store) put(ctx context.Context, path string, body any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("qdrant PUT %s: status %d", path, resp.StatusCode)
	}
	return nil
}

func (s *Store) post(ctx context.Context, path string, body any) ([]byte, error) {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("qdrant POST %s: status %d: %s", path, resp.StatusCode, buf.String())
	}
	return buf.Bytes(), nil
}
