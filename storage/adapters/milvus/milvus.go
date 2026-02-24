// Package milvus provides a Milvus-backed VectorStore adapter for Chronos.
package milvus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/spawn08/chronos/storage"
)

// Store implements storage.VectorStore using Milvus REST API (v2).
type Store struct {
	endpoint string
	token    string
	client   *http.Client
}

// New creates a Milvus vector store.
// endpoint is the Milvus REST endpoint, e.g. "http://localhost:19530".
func New(endpoint, token string) *Store {
	return &Store{
		endpoint: endpoint,
		token:    token,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *Store) doJSON(ctx context.Context, path string, body any) (json.RawMessage, error) {
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("milvus: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("milvus %s: %w", path, err)
	}
	defer resp.Body.Close()
	var result json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("milvus %s: status %d: %s", path, resp.StatusCode, string(result))
	}
	return result, nil
}

func (s *Store) CreateCollection(ctx context.Context, name string, dimension int) error {
	_, err := s.doJSON(ctx, "/v2/vectordb/collections/create", map[string]any{
		"collectionName": name,
		"dimension":      dimension,
		"metricType":     "COSINE",
	})
	return err
}

func (s *Store) Upsert(ctx context.Context, collection string, embeddings []storage.Embedding) error {
	data := make([]map[string]any, len(embeddings))
	for i, e := range embeddings {
		meta, _ := json.Marshal(e.Metadata)
		data[i] = map[string]any{
			"id":       e.ID,
			"vector":   e.Vector,
			"content":  e.Content,
			"metadata": string(meta),
		}
	}
	_, err := s.doJSON(ctx, "/v2/vectordb/entities/upsert", map[string]any{
		"collectionName": collection,
		"data":           data,
	})
	return err
}

func (s *Store) Search(ctx context.Context, collection string, query []float32, topK int) ([]storage.SearchResult, error) {
	raw, err := s.doJSON(ctx, "/v2/vectordb/entities/search", map[string]any{
		"collectionName": collection,
		"data":           [][]float32{query},
		"limit":          topK,
		"outputFields":   []string{"content", "metadata"},
	})
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data []struct {
			ID       string  `json:"id"`
			Distance float32 `json:"distance"`
			Content  string  `json:"content"`
			Metadata string  `json:"metadata"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &resp)

	results := make([]storage.SearchResult, len(resp.Data))
	for i, d := range resp.Data {
		var meta map[string]any
		_ = json.Unmarshal([]byte(d.Metadata), &meta)
		results[i] = storage.SearchResult{
			Embedding: storage.Embedding{
				ID:       d.ID,
				Content:  d.Content,
				Metadata: meta,
			},
			Score: 1 - d.Distance,
		}
	}
	return results, nil
}

func (s *Store) Delete(ctx context.Context, collection string, ids []string) error {
	_, err := s.doJSON(ctx, "/v2/vectordb/entities/delete", map[string]any{
		"collectionName": collection,
		"ids":            ids,
	})
	return err
}

func (s *Store) Close() error { return nil }
