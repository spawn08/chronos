// Package redisvector provides a Redis-backed VectorStore adapter using the
// RediSearch module (Redis Stack) for KNN vector similarity search.
//
// It is built on the official go-redis client (github.com/redis/go-redis/v9).
// The previous hand-rolled TCP implementation had two fatal correctness bugs:
//
//  1. It read replies with a single 64KB net.Conn.Read, so any FT.SEARCH result
//     larger than 64KB (trivial with vectors/metadata) was silently truncated
//     and mis-parsed.
//  2. It encoded vectors as a comma-separated ASCII string ("0.1,0.2,..."),
//     but RediSearch VECTOR fields and the KNN $BLOB parameter require raw
//     little-endian float32 bytes. The old adapter could never return a correct
//     match against a real RediSearch index.
//
// This rewrite delegates framing to go-redis and encodes vectors as binary
// float32 blobs. RESP2 is forced (Protocol: 2) so FT.SEARCH replies have the
// stable positional array shape that parseSearchReply understands.
//
// # Testing note
//
// RediSearch (FT.*) is a Redis module and is NOT emulated by miniredis, so the
// unit tests here cover the pure, wire-critical logic: binary vector encoding,
// search-query construction, and reply parsing. End-to-end coverage requires a
// live Redis Stack instance; set REDISVECTOR_ADDR to run TestIntegration.
package redisvector

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/redis/go-redis/v9"

	"github.com/spawn08/chronos/storage"
)

// scoreField is the alias RediSearch assigns to the KNN distance in results.
const scoreField = "__vector_score"

// Store implements storage.VectorStore using Redis with RediSearch.
type Store struct {
	client redis.UniversalClient
}

// New creates a RediSearch-backed vector store and verifies connectivity.
func New(addr string) (*Store, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Protocol: 2, // RESP2: stable positional FT.SEARCH reply shape.
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redisvector connect: %w", err)
	}
	return &Store{client: client}, nil
}

// NewWithClient wraps an existing go-redis client (useful for tests / custom opts).
func NewWithClient(client redis.UniversalClient) *Store {
	return &Store{client: client}
}

// encodeVector serializes a float32 slice to the raw little-endian byte blob
// that RediSearch expects for VECTOR fields and KNN $BLOB parameters.
func encodeVector(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func (s *Store) CreateCollection(ctx context.Context, name string, dimension int) error {
	args := []any{
		"FT.CREATE", name,
		"ON", "HASH",
		"PREFIX", "1", name + ":",
		"SCHEMA",
		"vector", "VECTOR", "FLAT", "6",
		"TYPE", "FLOAT32",
		"DIM", strconv.Itoa(dimension),
		"DISTANCE_METRIC", "COSINE",
		"content", "TEXT",
		"metadata", "TEXT",
	}
	if err := s.client.Do(ctx, args...).Err(); err != nil {
		return fmt.Errorf("redisvector create collection %q: %w", name, err)
	}
	return nil
}

func (s *Store) Upsert(ctx context.Context, collection string, embeddings []storage.Embedding) error {
	for _, e := range embeddings {
		meta, err := json.Marshal(e.Metadata)
		if err != nil {
			return fmt.Errorf("redisvector marshal metadata for %q: %w", e.ID, err)
		}
		key := collection + ":" + e.ID
		if err := s.client.HSet(ctx, key,
			"vector", encodeVector(e.Vector),
			"content", e.Content,
			"metadata", string(meta),
		).Err(); err != nil {
			return fmt.Errorf("redisvector upsert %q: %w", e.ID, err)
		}
	}
	return nil
}

func (s *Store) Search(ctx context.Context, collection string, query []float32, topK int) ([]storage.SearchResult, error) {
	args := searchArgs(collection, query, topK)
	reply, err := s.client.Do(ctx, args...).Result()
	if err != nil {
		return nil, fmt.Errorf("redisvector search: %w", err)
	}
	return parseSearchReply(reply, collection), nil
}

// searchArgs builds the FT.SEARCH command for a KNN query.
func searchArgs(collection string, query []float32, topK int) []any {
	return []any{
		"FT.SEARCH", collection,
		fmt.Sprintf("*=>[KNN %d @vector $BLOB AS %s]", topK, scoreField),
		"PARAMS", "2", "BLOB", encodeVector(query),
		"SORTBY", scoreField,
		"RETURN", "3", "content", "metadata", scoreField,
		"DIALECT", "2",
	}
}

// parseSearchReply converts a RESP2 FT.SEARCH reply into SearchResults.
//
// RESP2 shape: [ total, key1, [f1, v1, f2, v2, ...], key2, [ ... ], ... ].
// The cosine distance in scoreField is converted to a similarity score
// (1 - distance) to match the "higher is better" convention of SearchResult.
func parseSearchReply(reply any, collection string) []storage.SearchResult {
	arr, ok := reply.([]any)
	if !ok || len(arr) < 1 {
		return nil
	}

	prefix := collection + ":"
	var results []storage.SearchResult

	// arr[0] is the total count; documents follow as (key, fields) pairs.
	for i := 1; i+1 < len(arr); i += 2 {
		docKey, ok := asString(arr[i])
		if !ok || len(docKey) < len(prefix) || docKey[:len(prefix)] != prefix {
			continue
		}
		fields, ok := arr[i+1].([]any)
		if !ok {
			continue
		}

		result := storage.SearchResult{
			Embedding: storage.Embedding{
				ID:       docKey[len(prefix):],
				Metadata: make(map[string]any),
			},
		}
		for j := 0; j+1 < len(fields); j += 2 {
			name, _ := asString(fields[j])
			value, _ := asString(fields[j+1])
			switch name {
			case "content":
				result.Content = value
			case "metadata":
				_ = json.Unmarshal([]byte(value), &result.Metadata)
			case scoreField:
				if dist, err := strconv.ParseFloat(value, 32); err == nil {
					result.Score = 1.0 - float32(dist)
				}
			}
		}
		results = append(results, result)
	}
	return results
}

// asString normalizes RESP values (which may arrive as string or []byte).
func asString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case []byte:
		return string(t), true
	default:
		return "", false
	}
}

func (s *Store) Delete(ctx context.Context, collection string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = collection + ":" + id
	}
	if err := s.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("redisvector delete: %w", err)
	}
	return nil
}

func (s *Store) Close() error {
	if s.client != nil {
		if err := s.client.Close(); err != nil {
			return fmt.Errorf("redisvector close: %w", err)
		}
	}
	return nil
}

// Ensure Store implements storage.VectorStore at compile time.
var _ storage.VectorStore = (*Store)(nil)
