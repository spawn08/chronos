package main

import (
	"context"
	"hash/fnv"
	"strings"

	"github.com/spawn08/chronos/engine/model"
)

// hashEmbedder is a deterministic, offline model.EmbeddingsProvider. It maps
// each lowercase word of the input into a fixed-width bag-of-words vector by
// hashing the token to a dimension index. Documents that share vocabulary land
// close together under cosine similarity, which is enough to demonstrate RAG
// retrieval without any network calls or API keys.
//
// It is intentionally simple: real deployments should use a semantic
// embeddings provider (OpenAI, Gemini, a local model, …) via
// model.EmbeddingsProvider.
type hashEmbedder struct {
	dim int
}

func newHashEmbedder(dim int) *hashEmbedder {
	return &hashEmbedder{dim: dim}
}

func (h *hashEmbedder) Embed(_ context.Context, req *model.EmbeddingRequest) (*model.EmbeddingResponse, error) {
	out := make([][]float32, len(req.Input))
	for i, text := range req.Input {
		out[i] = h.vectorize(text)
	}
	return &model.EmbeddingResponse{Embeddings: out}, nil
}

func (h *hashEmbedder) vectorize(text string) []float32 {
	vec := make([]float32, h.dim)
	for _, tok := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return r < 'a' || r > 'z'
	}) {
		hsh := fnv.New32a()
		_, _ = hsh.Write([]byte(tok))
		// int(uint32) is always non-negative and fits a 64-bit int, so the
		// modulo index stays in [0, dim) without a signed/unsigned conversion.
		vec[int(hsh.Sum32())%h.dim]++
	}
	return vec
}
