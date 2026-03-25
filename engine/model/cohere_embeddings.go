package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// CohereEmbeddings implements EmbeddingsProvider using the Cohere Embed API.
type CohereEmbeddings struct {
	config ProviderConfig
	http   *httpClient
}

// NewCohereEmbeddings creates a Cohere embeddings provider.
// apiKey is the Cohere API key.
// modelID is the model identifier (e.g., "embed-english-v3.0", "embed-multilingual-v3.0").
func NewCohereEmbeddings(apiKey, modelID string) *CohereEmbeddings {
	return NewCohereEmbeddingsWithConfig(ProviderConfig{
		APIKey:  apiKey,
		BaseURL: "https://api.cohere.ai",
		Model:   modelID,
	})
}

// NewCohereEmbeddingsWithConfig creates a Cohere embeddings provider with full config.
func NewCohereEmbeddingsWithConfig(cfg ProviderConfig) *CohereEmbeddings {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.cohere.ai"
	}
	if cfg.Model == "" {
		cfg.Model = "embed-english-v3.0"
	}
	headers := map[string]string{
		"Authorization": "Bearer " + cfg.APIKey,
	}
	return &CohereEmbeddings{
		config: cfg,
		http:   newHTTPClient(cfg.BaseURL, cfg.TimeoutSec, headers),
	}
}

func (c *CohereEmbeddings) Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	modelID := req.Model
	if modelID == "" {
		modelID = c.config.Model
	}

	body := map[string]any{
		"model":      modelID,
		"texts":      req.Input,
		"input_type": "search_document",
	}

	resp, err := c.http.post(ctx, "/v1/embed", body)
	if err != nil {
		return nil, fmt.Errorf("cohere embeddings: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cohere embeddings: %s", readErrorBody(resp))
	}

	var raw cohereEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("cohere embeddings decode: %w", err)
	}

	embeddings := make([][]float32, len(raw.Embeddings))
	copy(embeddings, raw.Embeddings)

	return &EmbeddingResponse{
		Embeddings: embeddings,
		Usage: Usage{
			PromptTokens: raw.Meta.BilledUnits.InputTokens,
		},
	}, nil
}

type cohereEmbeddingResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Meta       struct {
		BilledUnits struct {
			InputTokens int `json:"input_tokens"`
		} `json:"billed_units"`
	} `json:"meta"`
}
