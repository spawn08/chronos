// Package redisvector provides a Redis-backed VectorStore adapter using RediSearch.
package redisvector

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spawn08/chronos/storage"
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
		return "", fmt.Errorf("redisvector write: %w", err)
	}
	buf := make([]byte, 65536)
	n, readErr := s.conn.Read(buf)
	if readErr != nil {
		return "", fmt.Errorf("redisvector read: %w", readErr)
	}
	resp := string(buf[:n])
	if resp != "" && resp[0] == '-' {
		return "", fmt.Errorf("redisvector error: %s", strings.TrimSpace(resp[1:]))
	}
	return resp, nil
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
	return ParseSearchResponse(resp, collection), nil
}

// ParseSearchResponse parses a RediSearch FT.SEARCH RESP response into SearchResults.
//
// FT.SEARCH response format (RESP2):
//
//	*<N>           — array header (1 + 2*num_results elements)
//	:<total>       — integer: total number of matches
//	$<len>         — bulk string: document key (e.g., "collection:id")
//	*<fields>      — array of field name-value pairs
//	  $<len> field_name
//	  $<len> field_value
//	  ...
//	$<len>         — next document key
//	*<fields>      — next document's fields
//	...
func ParseSearchResponse(resp, collection string) []storage.SearchResult {
	if resp == "" {
		return nil
	}

	lines := strings.Split(resp, "\r\n")
	if len(lines) < 2 {
		return nil
	}

	var results []storage.SearchResult
	i := 0

	// Skip the array header (*N) and total count (:N)
	for i < len(lines) {
		if lines[i] != "" && lines[i][0] == ':' {
			i++
			break
		}
		i++
	}

	prefix := collection + ":"
	for i < len(lines) {
		if i+1 < len(lines) && lines[i] != "" && lines[i][0] == '$' {
			docKey := lines[i+1]
			i += 2

			if !strings.HasPrefix(docKey, prefix) {
				continue
			}

			docID := strings.TrimPrefix(docKey, prefix)
			result := storage.SearchResult{
				Embedding: storage.Embedding{
					ID:       docID,
					Metadata: make(map[string]any),
				},
			}

			// Parse the field array: *N means N elements (N/2 name-value pairs)
			if i < len(lines) && lines[i] != "" && lines[i][0] == '*' {
				numElements := 0
				if n, err := strconv.Atoi(lines[i][1:]); err == nil {
					numElements = n
				}
				i++
				pairs := numElements / 2
				for p := 0; p < pairs && i+3 < len(lines); p++ {
					if lines[i][0] != '$' {
						break
					}
					fieldName := lines[i+1]
					i += 2
					if i >= len(lines) || lines[i][0] != '$' {
						break
					}
					fieldValue := lines[i+1]
					i += 2

					switch fieldName {
					case "content":
						result.Content = fieldValue
					case "metadata":
						_ = json.Unmarshal([]byte(fieldValue), &result.Metadata)
					case "__vector_score":
						if score, err := strconv.ParseFloat(fieldValue, 32); err == nil {
							result.Score = 1.0 - float32(score)
						}
					}
				}
			}

			results = append(results, result)
		} else {
			i++
		}
	}

	return results
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

// Ensure Store implements storage.VectorStore at compile time.
var _ storage.VectorStore = (*Store)(nil)
