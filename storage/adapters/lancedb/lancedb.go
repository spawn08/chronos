// Package lancedb provides a LanceDB-backed VectorStore adapter for Chronos.
// LanceDB uses a REST API for its cloud/server deployment.
package lancedb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spawn08/chronos/storage"
)

// Store implements storage.VectorStore using LanceDB's REST API.
type Store struct {
	baseURL string
	apiKey  string
	dbName  string
	client  *http.Client
}

// New creates a LanceDB vector store client.
// baseURL is the LanceDB Cloud or self-hosted server endpoint.
// apiKey is the API key for authentication (empty for local).
// dbName is the database name.
func New(baseURL, apiKey, dbName string) *Store {
	return &Store{
		baseURL: baseURL,
		apiKey:  apiKey,
		dbName:  dbName,
		client:  &http.Client{},
	}
}

func (s *Store) CreateCollection(ctx context.Context, name string, dimension int) error {
	body := map[string]any{
		"name":      name,
		"dimension": dimension,
		"metric":    "cosine",
	}
	_, err := s.doRequest(ctx, http.MethodPost,
		fmt.Sprintf("/db/%s/table/%s/create/", s.dbName, name), body)
	return err
}

func (s *Store) Upsert(ctx context.Context, collection string, embeddings []storage.Embedding) error {
	records := make([]map[string]any, len(embeddings))
	for i, e := range embeddings {
		meta, _ := json.Marshal(e.Metadata)
		records[i] = map[string]any{
			"id":       e.ID,
			"vector":   e.Vector,
			"content":  e.Content,
			"metadata": string(meta),
		}
	}

	body := map[string]any{
		"data": records,
		"mode": "overwrite",
	}
	_, err := s.doRequest(ctx, http.MethodPost,
		fmt.Sprintf("/db/%s/table/%s/insert/", s.dbName, collection), body)
	return err
}

func (s *Store) Search(ctx context.Context, collection string, query []float32, topK int) ([]storage.SearchResult, error) {
	body := map[string]any{
		"vector": query,
		"k":      topK,
	}

	data, err := s.doRequest(ctx, http.MethodPost,
		fmt.Sprintf("/db/%s/table/%s/search/", s.dbName, collection), body)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Results []struct {
			ID       string         `json:"id"`
			Vector   []float32      `json:"vector"`
			Content  string         `json:"content"`
			Metadata string         `json:"metadata"`
			Score    float32        `json:"_distance"`
		} `json:"results"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("lancedb search decode: %w", err)
	}

	results := make([]storage.SearchResult, len(resp.Results))
	for i, r := range resp.Results {
		var meta map[string]any
		if r.Metadata != "" {
			json.Unmarshal([]byte(r.Metadata), &meta)
		}
		results[i] = storage.SearchResult{
			Embedding: storage.Embedding{
				ID:       r.ID,
				Vector:   r.Vector,
				Content:  r.Content,
				Metadata: meta,
			},
			Score: 1 - r.Score, // convert distance to similarity
		}
	}

	return results, nil
}

func (s *Store) Delete(ctx context.Context, collection string, ids []string) error {
	body := map[string]any{
		"filter": fmt.Sprintf("id IN (%s)", quoteIDs(ids)),
	}
	_, err := s.doRequest(ctx, http.MethodPost,
		fmt.Sprintf("/db/%s/table/%s/delete/", s.dbName, collection), body)
	return err
}

func (s *Store) Close() error {
	return nil
}

func (s *Store) doRequest(ctx context.Context, method, path string, body any) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("lancedb marshal: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	url := s.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("lancedb: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("x-api-key", s.apiKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lancedb: %w", err)
	}
	defer resp.Body.Close()

	respData, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("lancedb %s: HTTP %d: %s", path, resp.StatusCode, respData)
	}

	return respData, nil
}

func quoteIDs(ids []string) string {
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = fmt.Sprintf("'%s'", id)
	}
	result := ""
	for i, q := range quoted {
		if i > 0 {
			result += ", "
		}
		result += q
	}
	return result
}
