package model

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
)

// EmbeddingRequest is input for embedding generation.
type EmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// EmbeddingResponse contains the generated embeddings.
type EmbeddingResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Usage      Usage       `json:"usage"`
}

// EmbeddingsProvider generates vector embeddings from text.
type EmbeddingsProvider interface {
	Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error)
}

// CachedEmbeddings wraps an EmbeddingsProvider with an in-memory cache.
type CachedEmbeddings struct {
	inner EmbeddingsProvider
	mu    sync.RWMutex
	cache map[string][]float32
}

func NewCachedEmbeddings(inner EmbeddingsProvider) *CachedEmbeddings {
	return &CachedEmbeddings{
		inner: inner,
		cache: make(map[string][]float32),
	}
}

func (c *CachedEmbeddings) Embed(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	var uncached []string
	var uncachedIdx []int
	results := make([][]float32, len(req.Input))

	c.mu.RLock()
	for i, text := range req.Input {
		key := cacheKey(req.Model, text)
		if vec, ok := c.cache[key]; ok {
			results[i] = vec
		} else {
			uncached = append(uncached, text)
			uncachedIdx = append(uncachedIdx, i)
		}
	}
	c.mu.RUnlock()

	if len(uncached) == 0 {
		return &EmbeddingResponse{Embeddings: results}, nil
	}

	resp, err := c.inner.Embed(ctx, &EmbeddingRequest{Model: req.Model, Input: uncached})
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	for j, idx := range uncachedIdx {
		results[idx] = resp.Embeddings[j]
		c.cache[cacheKey(req.Model, uncached[j])] = resp.Embeddings[j]
	}
	c.mu.Unlock()

	return &EmbeddingResponse{Embeddings: results, Usage: resp.Usage}, nil
}

func cacheKey(model, text string) string {
	h := sha256.Sum256([]byte(model + ":" + text))
	return fmt.Sprintf("%x", h[:16])
}
