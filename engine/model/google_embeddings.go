package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// GoogleEmbeddings implements EmbeddingsProvider using the Google AI Gemini embeddings API.
type GoogleEmbeddings struct {
	config ProviderConfig
	http   *httpClient
}

// NewGoogleEmbeddings creates a Google AI embeddings provider.
// apiKey is the Google AI API key.
// modelID is the model identifier (e.g., "text-embedding-004", "embedding-001").
func NewGoogleEmbeddings(apiKey, modelID string) *GoogleEmbeddings {
	return NewGoogleEmbeddingsWithConfig(ProviderConfig{
		APIKey:  apiKey,
		BaseURL: "https://generativelanguage.googleapis.com",
		Model:   modelID,
	})
}

// NewGoogleEmbeddingsWithConfig creates a Google AI embeddings provider with full config.
func NewGoogleEmbeddingsWithConfig(cfg ProviderConfig) *GoogleEmbeddings {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://generativelanguage.googleapis.com"
	}
	if cfg.Model == "" {
		cfg.Model = "text-embedding-004"
	}
	return &GoogleEmbeddings{
		config: cfg,
		http:   newHTTPClient(cfg.BaseURL, cfg.TimeoutSec, nil),
	}
}

func (g *GoogleEmbeddings) Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	modelID := req.Model
	if modelID == "" {
		modelID = g.config.Model
	}

	// Google's batch embed API
	requests := make([]map[string]any, len(req.Input))
	for i, text := range req.Input {
		requests[i] = map[string]any{
			"model": fmt.Sprintf("models/%s", modelID),
			"content": map[string]any{
				"parts": []map[string]any{
					{"text": text},
				},
			},
		}
	}

	body := map[string]any{
		"requests": requests,
	}

	path := fmt.Sprintf("/v1beta/models/%s:batchEmbedContents?key=%s", modelID, g.config.APIKey)

	resp, err := g.http.post(ctx, path, body)
	if err != nil {
		return nil, fmt.Errorf("google embeddings: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google embeddings: %s", readErrorBody(resp))
	}

	var raw googleEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("google embeddings decode: %w", err)
	}

	embeddings := make([][]float32, len(raw.Embeddings))
	for i, emb := range raw.Embeddings {
		embeddings[i] = emb.Values
	}

	return &EmbeddingResponse{
		Embeddings: embeddings,
	}, nil
}

type googleEmbeddingResponse struct {
	Embeddings []struct {
		Values []float32 `json:"values"`
	} `json:"embeddings"`
}
