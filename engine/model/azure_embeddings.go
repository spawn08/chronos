package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// AzureOpenAIEmbeddings implements EmbeddingsProvider using Azure OpenAI's embeddings API.
type AzureOpenAIEmbeddings struct {
	config     ProviderConfig
	deployment string
	apiVersion string
	http       *httpClient
}

// NewAzureOpenAIEmbeddings creates an Azure OpenAI embeddings provider.
// endpoint is the Azure OpenAI resource endpoint (e.g., "https://myresource.openai.azure.com").
// apiKey is the Azure API key.
// deployment is the deployment name (e.g., "text-embedding-3-large").
func NewAzureOpenAIEmbeddings(endpoint, apiKey, deployment string) *AzureOpenAIEmbeddings {
	return NewAzureOpenAIEmbeddingsWithConfig(ProviderConfig{
		APIKey:  apiKey,
		BaseURL: endpoint,
		Model:   deployment,
	}, deployment, "2024-02-01")
}

// NewAzureOpenAIEmbeddingsWithConfig creates an Azure OpenAI embeddings provider with full config.
func NewAzureOpenAIEmbeddingsWithConfig(cfg ProviderConfig, deployment, apiVersion string) *AzureOpenAIEmbeddings {
	if apiVersion == "" {
		apiVersion = "2024-02-01"
	}
	headers := map[string]string{
		"api-key": cfg.APIKey,
	}
	return &AzureOpenAIEmbeddings{
		config:     cfg,
		deployment: deployment,
		apiVersion: apiVersion,
		http:       newHTTPClient(cfg.BaseURL, cfg.TimeoutSec, headers),
	}
}

func (a *AzureOpenAIEmbeddings) Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	body := map[string]any{
		"input": req.Input,
	}

	path := fmt.Sprintf("/openai/deployments/%s/embeddings?api-version=%s", a.deployment, a.apiVersion)

	resp, err := a.http.post(ctx, path, body)
	if err != nil {
		return nil, fmt.Errorf("azure embeddings: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("azure embeddings: %s", readErrorBody(resp))
	}

	var oaiResp openAIEmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&oaiResp); err != nil {
		return nil, fmt.Errorf("azure embeddings decode: %w", err)
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
