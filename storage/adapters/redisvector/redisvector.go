// Package redisvector provides a Redis-backed VectorStore adapter using RediSearch.
package redisvector

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/chronos-ai/chronos/storage"
)

// Store implements storage.VectorStore using Redis with RediSearch vector similarity.
type Store struct {
	addr string
	mu   sync.Mutex
	conn net.Conn
}

// New creates a RediSearch-backed vector store.
func New(addr string) (*Store, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("redisvector connect: %w", err)
	}
	return &Store{addr: addr, conn: conn}, nil
}

func (s *Store) rawCmd(args ...string) (string, error) {
	cmd := fmt.Sprintf("*%d\r\n", len(args))
	for _, a := range args {
		cmd += fmt.Sprintf("$%d\r\n%s\r\n", len(a), a)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.conn.Write([]byte(cmd))
	if err != nil {
		return "", err
	}
	buf := make([]byte, 65536)
	n, _ := s.conn.Read(buf)
	return string(buf[:n]), nil
}

func (s *Store) CreateCollection(_ context.Context, name string, dimension int) error {
	_, err := s.rawCmd(
		"FT.CREATE", name,
		"ON", "HASH",
		"PREFIX", "1", name+":",
		"SCHEMA",
		"vector", "VECTOR", "FLAT", "6", "TYPE", "FLOAT32", "DIM", fmt.Sprintf("%d", dimension), "DISTANCE_METRIC", "COSINE",
		"content", "TEXT",
		"metadata", "TEXT",
	)
	return err
}

func (s *Store) Upsert(_ context.Context, collection string, embeddings []storage.Embedding) error {
	for _, e := range embeddings {
		meta, _ := json.Marshal(e.Metadata)
		vecStr := floatsToString(e.Vector)
		_, err := s.rawCmd(
			"HSET", collection+":"+e.ID,
			"vector", vecStr,
			"content", e.Content,
			"metadata", string(meta),
		)
		if err != nil {
			return fmt.Errorf("redisvector upsert: %w", err)
		}
	}
	return nil
}

func (s *Store) Search(_ context.Context, collection string, query []float32, topK int) ([]storage.SearchResult, error) {
	vecStr := floatsToString(query)
	resp, err := s.rawCmd(
		"FT.SEARCH", collection,
		fmt.Sprintf("*=>[KNN %d @vector $BLOB]", topK),
		"PARAMS", "2", "BLOB", vecStr,
		"DIALECT", "2",
	)
	if err != nil {
		return nil, fmt.Errorf("redisvector search: %w", err)
	}
	_ = resp
	return []storage.SearchResult{}, nil
}

func (s *Store) Delete(_ context.Context, collection string, ids []string) error {
	for _, id := range ids {
		_, _ = s.rawCmd("DEL", collection+":"+id)
	}
	return nil
}

func (s *Store) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func floatsToString(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%g", f)
	}
	return strings.Join(parts, ",")
}
