package knowledge

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/storage"
)

// VectorKnowledge implements Knowledge backed by a VectorStore and EmbeddingsProvider.
type VectorKnowledge struct {
	Collection string
	Dimension  int
	Store      storage.VectorStore
	Embedder   model.EmbeddingsProvider
	EmbedModel string
	documents  []Document // raw documents to index
}

// NewVectorKnowledge creates a vector-backed knowledge base.
func NewVectorKnowledge(collection string, dimension int, store storage.VectorStore, embedder model.EmbeddingsProvider, embedModel string) *VectorKnowledge {
	return &VectorKnowledge{
		Collection: collection,
		Dimension:  dimension,
		Store:      store,
		Embedder:   embedder,
		EmbedModel: embedModel,
	}
}

// AddDocuments queues documents for indexing on next Load() call.
func (v *VectorKnowledge) AddDocuments(docs ...Document) {
	v.documents = append(v.documents, docs...)
}

// Load creates the collection and indexes all queued documents.
func (v *VectorKnowledge) Load(ctx context.Context) error {
	if err := v.Store.CreateCollection(ctx, v.Collection, v.Dimension); err != nil {
		return fmt.Errorf("knowledge load: create collection: %w", err)
	}

	if len(v.documents) == 0 {
		return nil
	}

	// Batch embed all document contents
	texts := make([]string, len(v.documents))
	for i, d := range v.documents {
		texts[i] = d.Content
	}

	resp, err := v.Embedder.Embed(ctx, &model.EmbeddingRequest{
		Model: v.EmbedModel,
		Input: texts,
	})
	if err != nil {
		return fmt.Errorf("knowledge load: embed: %w", err)
	}

	embeddings := make([]storage.Embedding, len(v.documents))
	for i, doc := range v.documents {
		id := doc.ID
		if id == "" {
			h := sha256.Sum256([]byte(doc.Content))
			id = fmt.Sprintf("%x", h[:16])
		}
		embeddings[i] = storage.Embedding{
			ID:       id,
			Vector:   resp.Embeddings[i],
			Metadata: doc.Metadata,
			Content:  doc.Content,
		}
	}

	return v.Store.Upsert(ctx, v.Collection, embeddings)
}

// Search embeds the query and performs similarity search.
func (v *VectorKnowledge) Search(ctx context.Context, query string, topK int) ([]Document, error) {
	resp, err := v.Embedder.Embed(ctx, &model.EmbeddingRequest{
		Model: v.EmbedModel,
		Input: []string{query},
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge search: embed query: %w", err)
	}

	results, err := v.Store.Search(ctx, v.Collection, resp.Embeddings[0], topK)
	if err != nil {
		return nil, fmt.Errorf("knowledge search: %w", err)
	}

	docs := make([]Document, len(results))
	for i, r := range results {
		docs[i] = Document{
			ID:       r.ID,
			Content:  r.Content,
			Metadata: r.Metadata,
			Score:    r.Score,
		}
	}
	return docs, nil
}

func (v *VectorKnowledge) Close() error {
	return v.Store.Close()
}
