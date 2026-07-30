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

// defaultUpsertBatch is the maximum number of points sent in one Upsert request.
// Large corpora are chunked into batches of this size so a single request never
// grows unbounded.
const defaultUpsertBatch = 256

// Store implements storage.VectorStore using Qdrant's REST API.
type Store struct {
	baseURL     string
	client      *http.Client
	upsertBatch int
}

// New creates a Qdrant vector store client.
func New(baseURL string) *Store {
	return &Store{
		baseURL:     baseURL,
		client:      &http.Client{},
		upsertBatch: defaultUpsertBatch,
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
	if len(embeddings) == 0 {
		return nil
	}
	batch := s.upsertBatch
	if batch <= 0 {
		batch = defaultUpsertBatch
	}
	path := fmt.Sprintf("/collections/%s/points", collection)
	for start := 0; start < len(embeddings); start += batch {
		end := start + batch
		if end > len(embeddings) {
			end = len(embeddings)
		}
		chunk := embeddings[start:end]
		points := make([]map[string]any, len(chunk))
		for i, e := range chunk {
			// Copy metadata so we never mutate the caller's map.
			payload := make(map[string]any, len(e.Metadata)+1)
			for k, v := range e.Metadata {
				payload[k] = v
			}
			payload["_content"] = e.Content
			points[i] = map[string]any{
				"id":      e.ID,
				"vector":  e.Vector,
				"payload": payload,
			}
		}
		if err := s.put(ctx, path, map[string]any{"points": points}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Search(ctx context.Context, collection string, query []float32, topK int, opts ...storage.SearchOption) ([]storage.SearchResult, error) {
	body := map[string]any{
		"vector":       query,
		"limit":        topK,
		"with_payload": true,
	}
	// Server-side payload filter so top-k is selected within the matching subset.
	if f := storage.ApplySearchOptions(opts...).Filter; len(f) > 0 {
		must := make([]map[string]any, 0, len(f))
		for k, v := range f {
			must = append(must, map[string]any{"key": k, "match": map[string]any{"value": v}})
		}
		body["filter"] = map[string]any{"must": must}
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
	_, _ = buf.ReadFrom(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("qdrant POST %s: status %d: %s", path, resp.StatusCode, buf.String())
	}
	return buf.Bytes(), nil
}
