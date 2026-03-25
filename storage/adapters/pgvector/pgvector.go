// Package pgvector provides a PostgreSQL+pgvector-backed VectorStore adapter for Chronos.
package pgvector

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spawn08/chronos/storage"
)

// Store implements storage.VectorStore using PostgreSQL with the pgvector extension.
type Store struct {
	db *sql.DB
}

// New creates a PgVector store from an existing database connection.
// The database must have the pgvector extension enabled: CREATE EXTENSION IF NOT EXISTS vector;
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) CreateCollection(ctx context.Context, name string, dimension int) error {
	// Enable pgvector extension if not already enabled
	if _, err := s.db.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		return fmt.Errorf("pgvector enable extension: %w", err)
	}

	query := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id TEXT PRIMARY KEY,
		embedding vector(%d),
		content TEXT DEFAULT '',
		metadata JSONB DEFAULT '{}'
	)`, sanitizeTableName(name), dimension)

	if _, err := s.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("pgvector create collection %q: %w", name, err)
	}

	// Create HNSW index for cosine similarity
	indexQuery := fmt.Sprintf(
		`CREATE INDEX IF NOT EXISTS %s_embedding_idx ON %s USING hnsw (embedding vector_cosine_ops)`,
		sanitizeTableName(name), sanitizeTableName(name))
	if _, err := s.db.ExecContext(ctx, indexQuery); err != nil {
		return fmt.Errorf("pgvector create index: %w", err)
	}

	return nil
}

func (s *Store) Upsert(ctx context.Context, collection string, embeddings []storage.Embedding) error {
	table := sanitizeTableName(collection)

	for _, e := range embeddings {
		meta, err := json.Marshal(e.Metadata)
		if err != nil {
			return fmt.Errorf("pgvector marshal metadata: %w", err)
		}

		vecStr := vectorToString(e.Vector)

		query := fmt.Sprintf(`INSERT INTO %s (id, embedding, content, metadata)
			VALUES ($1, $2::vector, $3, $4::jsonb)
			ON CONFLICT (id) DO UPDATE SET
				embedding = EXCLUDED.embedding,
				content = EXCLUDED.content,
				metadata = EXCLUDED.metadata`, table)

		if _, err := s.db.ExecContext(ctx, query, e.ID, vecStr, e.Content, string(meta)); err != nil {
			return fmt.Errorf("pgvector upsert: %w", err)
		}
	}
	return nil
}

func (s *Store) Search(ctx context.Context, collection string, query []float32, topK int) ([]storage.SearchResult, error) {
	table := sanitizeTableName(collection)
	vecStr := vectorToString(query)

	sqlQuery := fmt.Sprintf(`SELECT id, embedding::text, content, metadata,
		1 - (embedding <=> $1::vector) AS score
		FROM %s
		ORDER BY embedding <=> $1::vector
		LIMIT $2`, table)

	rows, err := s.db.QueryContext(ctx, sqlQuery, vecStr, topK)
	if err != nil {
		return nil, fmt.Errorf("pgvector search: %w", err)
	}
	defer rows.Close()

	var results []storage.SearchResult
	for rows.Next() {
		var r storage.SearchResult
		var vecText, metaJSON string

		if err := rows.Scan(&r.ID, &vecText, &r.Content, &metaJSON, &r.Score); err != nil {
			return nil, fmt.Errorf("pgvector search scan: %w", err)
		}

		if metaJSON != "" {
			json.Unmarshal([]byte(metaJSON), &r.Metadata)
		}
		results = append(results, r)
	}

	return results, rows.Err()
}

func (s *Store) Delete(ctx context.Context, collection string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	table := sanitizeTableName(collection)

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	query := fmt.Sprintf(`DELETE FROM %s WHERE id IN (%s)`, table, strings.Join(placeholders, ","))
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("pgvector delete: %w", err)
	}
	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func vectorToString(vec []float32) string {
	parts := make([]string, len(vec))
	for i, v := range vec {
		parts[i] = fmt.Sprintf("%g", v)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func sanitizeTableName(name string) string {
	// Simple sanitization: allow only alphanumeric and underscore
	var b strings.Builder
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			b.WriteRune(c)
		}
	}
	result := b.String()
	if result == "" {
		return "default_collection"
	}
	return result
}
