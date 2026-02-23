// Package redisvector provides a Redis-backed VectorStore adapter stub.
package redisvector

import (
	"context"
	"fmt"

	"github.com/chronos-ai/chronos/storage"
)

// Store implements storage.VectorStore using Redis with RediSearch.
type Store struct {
	addr string
}

func New(addr string) *Store { return &Store{addr: addr} }

func (s *Store) CreateCollection(_ context.Context, name string, dimension int) error {
	// TODO: FT.CREATE index with VECTOR field
	return fmt.Errorf("redisvector: not yet implemented")
}

func (s *Store) Upsert(_ context.Context, _ string, _ []storage.Embedding) error {
	return fmt.Errorf("redisvector: not yet implemented")
}

func (s *Store) Search(_ context.Context, _ string, _ []float32, _ int) ([]storage.SearchResult, error) {
	return nil, fmt.Errorf("redisvector: not yet implemented")
}

func (s *Store) Delete(_ context.Context, _ string, _ []string) error {
	return fmt.Errorf("redisvector: not yet implemented")
}

func (s *Store) Close() error { return nil }
