// Package pinecone provides a Pinecone-backed VectorStore adapter stub.
package pinecone

// Store will implement storage.VectorStore using Pinecone.
type Store struct {
	APIKey      string
	Environment string
	IndexName   string
}

// TODO: Implement storage.VectorStore interface methods.
