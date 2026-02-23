package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// OllamaEmbeddings implements EmbeddingsProvider using a local Ollama server.
type OllamaEmbeddings struct {
	http  *httpClient
	model string
}

// NewOllamaEmbeddings creates an Ollama embeddings provider.
func NewOllamaEmbeddings(baseURL, modelID string) *OllamaEmbeddings {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if modelID == "" {
		modelID = "nomic-embed-text"
	}
	return &OllamaEmbeddings{
		http:  newHTTPClient(baseURL, 120, nil),
		model: modelID,
	}
}

func (o *OllamaEmbeddings) Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	modelID := req.Model
	if modelID == "" {
		modelID = o.model
	}

	embeddings := make([][]float32, 0, len(req.Input))
	for _, text := range req.Input {
		body := map[string]any{
			"model":  modelID,
			"prompt": text,
		}

		resp, err := o.http.post(ctx, "/api/embeddings", body)
		if err != nil {
			return nil, fmt.Errorf("ollama embeddings: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			errMsg := readErrorBody(resp)
			resp.Body.Close()
			return nil, fmt.Errorf("ollama embeddings: %s", errMsg)
		}

		var ollamaResp struct {
			Embedding []float32 `json:"embedding"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("ollama embeddings decode: %w", err)
		}
		resp.Body.Close()
		embeddings = append(embeddings, ollamaResp.Embedding)
	}

	return &EmbeddingResponse{Embeddings: embeddings}, nil
}
