// Package weaviate provides a Weaviate-backed VectorStore adapter for Chronos.
package weaviate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/chronos-ai/chronos/storage"
)

// Store implements storage.VectorStore using Weaviate's REST API.
type Store struct {
	endpoint string
	apiKey   string
	client   *http.Client
}

// New creates a Weaviate vector store.
func New(endpoint, apiKey string) *Store {
	return &Store{
		endpoint: endpoint,
		apiKey:   apiKey,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *Store) doJSON(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.endpoint+path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("weaviate: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("weaviate %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	var result json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("weaviate %s: status %d: %s", path, resp.StatusCode, string(result))
	}
	return result, nil
}

func (s *Store) CreateCollection(ctx context.Context, name string, dimension int) error {
	schema := map[string]any{
		"class": name,
		"vectorizer": "none",
		"properties": []map[string]any{
			{"name": "content", "dataType": []string{"text"}},
			{"name": "meta", "dataType": []string{"text"}},
		},
	}
	_, err := s.doJSON(ctx, http.MethodPost, "/v1/schema", schema)
	return err
}

func (s *Store) Upsert(ctx context.Context, collection string, embeddings []storage.Embedding) error {
	for _, e := range embeddings {
		meta, _ := json.Marshal(e.Metadata)
		obj := map[string]any{
			"class": collection,
			"id":    e.ID,
			"properties": map[string]any{
				"content": e.Content,
				"meta":    string(meta),
			},
			"vector": e.Vector,
		}
		_, err := s.doJSON(ctx, http.MethodPost, "/v1/objects", obj)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Search(ctx context.Context, collection string, query []float32, topK int) ([]storage.SearchResult, error) {
	vecJSON, _ := json.Marshal(query)
	gql := fmt.Sprintf(`{"query":"{Get{%s(nearVector:{vector:%s},limit:%d){content meta _additional{id distance}}}}"}`,
		collection, string(vecJSON), topK)

	raw, err := s.doJSON(ctx, http.MethodPost, "/v1/graphql", json.RawMessage(gql))
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data struct {
			Get map[string][]struct {
				Content    string `json:"content"`
				Meta       string `json:"meta"`
				Additional struct {
					ID       string  `json:"id"`
					Distance float32 `json:"distance"`
				} `json:"_additional"`
			} `json:"Get"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &resp)

	items := resp.Data.Get[collection]
	results := make([]storage.SearchResult, len(items))
	for i, item := range items {
		var meta map[string]any
		_ = json.Unmarshal([]byte(item.Meta), &meta)
		results[i] = storage.SearchResult{
			Embedding: storage.Embedding{
				ID:       item.Additional.ID,
				Content:  item.Content,
				Metadata: meta,
			},
			Score: 1 - item.Additional.Distance,
		}
	}
	return results, nil
}

func (s *Store) Delete(ctx context.Context, collection string, ids []string) error {
	for _, id := range ids {
		_, _ = s.doJSON(ctx, http.MethodDelete, fmt.Sprintf("/v1/objects/%s/%s", collection, id), nil)
	}
	return nil
}

func (s *Store) Close() error { return nil }
