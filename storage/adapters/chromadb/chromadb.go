// Package chromadb provides a ChromaDB-backed VectorStore adapter for Chronos.
package chromadb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spawn08/chronos/storage"
)

// Store implements storage.VectorStore using ChromaDB's REST API.
type Store struct {
	baseURL string
	client  *http.Client
	tenant  string
	db      string
}

// New creates a ChromaDB vector store client.
// baseURL is the ChromaDB server address (e.g., "http://localhost:8000").
func New(baseURL string) *Store {
	return &Store{
		baseURL: baseURL,
		client:  &http.Client{},
		tenant:  "default_tenant",
		db:      "default_database",
	}
}

// WithTenant sets the tenant for multi-tenant deployments.
func (s *Store) WithTenant(tenant string) *Store {
	s.tenant = tenant
	return s
}

// WithDatabase sets the database name.
func (s *Store) WithDatabase(db string) *Store {
	s.db = db
	return s
}

func (s *Store) CreateCollection(ctx context.Context, name string, dimension int) error {
	body := map[string]any{
		"name": name,
		"metadata": map[string]any{
			"hnsw:space": "cosine",
			"dimension":  dimension,
		},
	}
	_, err := s.post(ctx, fmt.Sprintf("/api/v1/tenants/%s/databases/%s/collections", s.tenant, s.db), body)
	return err
}

func (s *Store) Upsert(ctx context.Context, collection string, embeddings []storage.Embedding) error {
	collectionID, err := s.getCollectionID(ctx, collection)
	if err != nil {
		return fmt.Errorf("chromadb upsert: %w", err)
	}

	ids := make([]string, len(embeddings))
	vectors := make([][]float32, len(embeddings))
	metadatas := make([]map[string]any, len(embeddings))
	documents := make([]string, len(embeddings))

	for i, e := range embeddings {
		ids[i] = e.ID
		vectors[i] = e.Vector
		metadatas[i] = e.Metadata
		if metadatas[i] == nil {
			metadatas[i] = map[string]any{}
		}
		documents[i] = e.Content
	}

	body := map[string]any{
		"ids":        ids,
		"embeddings": vectors,
		"metadatas":  metadatas,
		"documents":  documents,
	}
	_, err = s.post(ctx, fmt.Sprintf("/api/v1/collections/%s/upsert", collectionID), body)
	return err
}

func (s *Store) Search(ctx context.Context, collection string, query []float32, topK int) ([]storage.SearchResult, error) {
	collectionID, err := s.getCollectionID(ctx, collection)
	if err != nil {
		return nil, fmt.Errorf("chromadb search: %w", err)
	}

	body := map[string]any{
		"query_embeddings": [][]float32{query},
		"n_results":        topK,
		"include":          []string{"embeddings", "metadatas", "documents", "distances"},
	}

	data, err := s.post(ctx, fmt.Sprintf("/api/v1/collections/%s/query", collectionID), body)
	if err != nil {
		return nil, err
	}

	var resp struct {
		IDs        [][]string         `json:"ids"`
		Embeddings [][][]float32      `json:"embeddings"`
		Metadatas  [][]map[string]any `json:"metadatas"`
		Documents  [][]string         `json:"documents"`
		Distances  [][]float32        `json:"distances"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("chromadb search decode: %w", err)
	}

	if len(resp.IDs) == 0 || len(resp.IDs[0]) == 0 {
		return nil, nil
	}

	results := make([]storage.SearchResult, len(resp.IDs[0]))
	for i := range resp.IDs[0] {
		r := storage.SearchResult{
			Embedding: storage.Embedding{
				ID:       resp.IDs[0][i],
				Metadata: resp.Metadatas[0][i],
			},
		}
		if len(resp.Embeddings) > 0 && len(resp.Embeddings[0]) > i {
			r.Vector = resp.Embeddings[0][i]
		}
		if len(resp.Documents) > 0 && len(resp.Documents[0]) > i {
			r.Content = resp.Documents[0][i]
		}
		if len(resp.Distances) > 0 && len(resp.Distances[0]) > i {
			// ChromaDB returns distances; convert to similarity score (1 - distance for cosine)
			r.Score = 1 - resp.Distances[0][i]
		}
		results[i] = r
	}

	return results, nil
}

func (s *Store) Delete(ctx context.Context, collection string, ids []string) error {
	collectionID, err := s.getCollectionID(ctx, collection)
	if err != nil {
		return fmt.Errorf("chromadb delete: %w", err)
	}

	body := map[string]any{
		"ids": ids,
	}
	_, err = s.post(ctx, fmt.Sprintf("/api/v1/collections/%s/delete", collectionID), body)
	return err
}

func (s *Store) Close() error {
	return nil
}

func (s *Store) getCollectionID(ctx context.Context, name string) (string, error) {
	url := fmt.Sprintf("%s/api/v1/tenants/%s/databases/%s/collections/%s", s.baseURL, s.tenant, s.db, name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("chromadb: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("chromadb: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("chromadb get collection %q: HTTP %d: %s", name, resp.StatusCode, body)
	}

	var col struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&col); err != nil {
		return "", fmt.Errorf("chromadb decode: %w", err)
	}
	return col.ID, nil
}

func (s *Store) post(ctx context.Context, path string, body any) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("chromadb marshal: %w", err)
	}

	url := s.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("chromadb: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chromadb: %w", err)
	}
	defer resp.Body.Close()

	respData, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("chromadb %s: HTTP %d: %s", path, resp.StatusCode, respData)
	}

	return respData, nil
}
