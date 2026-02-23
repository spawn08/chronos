package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// OpenAIEmbeddings implements EmbeddingsProvider using the OpenAI embeddings API.
type OpenAIEmbeddings struct {
	config ProviderConfig
	http   *httpClient
}

// NewOpenAIEmbeddings creates an OpenAI embeddings provider with the given API key.
func NewOpenAIEmbeddings(apiKey string) *OpenAIEmbeddings {
	return NewOpenAIEmbeddingsWithConfig(ProviderConfig{
		APIKey:  apiKey,
		BaseURL: "https://api.openai.com/v1",
		Model:   "text-embedding-3-small",
	})
}

// NewOpenAIEmbeddingsWithConfig creates an OpenAI embeddings provider with full config.
func NewOpenAIEmbeddingsWithConfig(cfg ProviderConfig) *OpenAIEmbeddings {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.Model == "" {
		cfg.Model = "text-embedding-3-small"
	}
	headers := map[string]string{
		"Authorization": "Bearer " + cfg.APIKey,
	}
	if cfg.OrgID != "" {
		headers["OpenAI-Organization"] = cfg.OrgID
	}
	return &OpenAIEmbeddings{
		config: cfg,
		http:   newHTTPClient(cfg.BaseURL, cfg.TimeoutSec, headers),
	}
}

func (o *OpenAIEmbeddings) Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	modelID := req.Model
	if modelID == "" {
		modelID = o.config.Model
	}

	body := map[string]any{
		"model": modelID,
		"input": req.Input,
	}

	resp, err := o.http.post(ctx, "/embeddings", body)
	if err != nil {
		return nil, fmt.Errorf("openai embeddings: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai embeddings: %s", readErrorBody(resp))
	}

	var oaiResp openAIEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("openai embeddings decode: %w", err)
	}

	embeddings := make([][]float32, len(oaiResp.Data))
	for i, d := range oaiResp.Data {
		embeddings[i] = d.Embedding
	}

	return &EmbeddingResponse{
		Embeddings: embeddings,
		Usage: Usage{
			PromptTokens: oaiResp.Usage.PromptTokens,
		},
	}, nil
}

type openAIEmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}
