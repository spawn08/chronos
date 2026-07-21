package redisvector

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"reflect"
	"testing"

	goredis "github.com/redis/go-redis/v9"

	"github.com/spawn08/chronos/storage"
)

func TestEncodeVector(t *testing.T) {
	tests := []struct {
		name string
		in   []float32
	}{
		{"empty", []float32{}},
		{"single", []float32{1.5}},
		{"multiple", []float32{0.1, 0.2, 0.3}},
		{"negatives", []float32{-1, 0, 1.25}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := encodeVector(tt.in)
			if len(got) != len(tt.in)*4 {
				t.Fatalf("len = %d, want %d", len(got), len(tt.in)*4)
			}
			// Round-trip decode and compare.
			for i, want := range tt.in {
				bits := binary.LittleEndian.Uint32(got[i*4:])
				if f := math.Float32frombits(bits); f != want {
					t.Errorf("elem %d = %v, want %v", i, f, want)
				}
			}
		})
	}
}

func TestSearchArgs(t *testing.T) {
	args := searchArgs("mycol", []float32{0.1, 0.2}, 5)
	if args[0] != "FT.SEARCH" || args[1] != "mycol" {
		t.Fatalf("unexpected head: %v %v", args[0], args[1])
	}
	// KNN query string with alias.
	if got := args[2].(string); got != "*=>[KNN 5 @vector $BLOB AS __vector_score]" {
		t.Errorf("query = %q", got)
	}
	// BLOB parameter must be the raw 8-byte (2 x float32) binary blob.
	blob, ok := args[6].([]byte)
	if !ok {
		t.Fatalf("BLOB param type = %T, want []byte", args[6])
	}
	if len(blob) != 8 {
		t.Errorf("BLOB len = %d, want 8", len(blob))
	}
	// DIALECT 2 required for KNN param syntax.
	if args[len(args)-2] != "DIALECT" || args[len(args)-1] != "2" {
		t.Errorf("missing DIALECT 2 tail: %v", args[len(args)-2:])
	}
}

func TestParseSearchReply(t *testing.T) {
	tests := []struct {
		name       string
		reply      any
		collection string
		wantIDs    []string
		wantScores []float32
	}{
		{
			name:       "nil reply",
			reply:      nil,
			collection: "c",
			wantIDs:    nil,
		},
		{
			name:       "no matches",
			reply:      []any{int64(0)},
			collection: "c",
			wantIDs:    nil,
		},
		{
			name: "two results with scores",
			reply: []any{
				int64(2),
				"mycol:doc1", []any{"content", "hello world", "metadata", "{}", scoreField, "0.1"},
				"mycol:doc2", []any{"content", "second", "metadata", `{"k":"v"}`, scoreField, "0.3"},
			},
			collection: "mycol",
			wantIDs:    []string{"doc1", "doc2"},
			wantScores: []float32{0.9, 0.7},
		},
		{
			name: "skips wrong prefix",
			reply: []any{
				int64(1),
				"othercol:x", []any{"content", "z"},
			},
			collection: "mycol",
			wantIDs:    nil,
		},
		{
			name: "byte-slice values",
			reply: []any{
				int64(1),
				[]byte("mycol:doc1"), []any{[]byte("content"), []byte("hi")},
			},
			collection: "mycol",
			wantIDs:    []string{"doc1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSearchReply(tt.reply, tt.collection)
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("got %d results, want %d", len(got), len(tt.wantIDs))
			}
			for i, want := range tt.wantIDs {
				if got[i].ID != want {
					t.Errorf("result[%d].ID = %q, want %q", i, got[i].ID, want)
				}
				if i < len(tt.wantScores) {
					if d := got[i].Score - tt.wantScores[i]; d < -0.001 || d > 0.001 {
						t.Errorf("result[%d].Score = %f, want %f", i, got[i].Score, tt.wantScores[i])
					}
				}
			}
		})
	}
}

func TestParseSearchReply_ContentAndMetadata(t *testing.T) {
	reply := []any{
		int64(1),
		"col:id1", []any{"content", "Hello, world!", "metadata", `{"source":"web.pdf"}`},
	}
	got := parseSearchReply(reply, "col")
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	if got[0].Content != "Hello, world!" {
		t.Errorf("content = %q", got[0].Content)
	}
	if src, ok := got[0].Metadata["source"]; !ok || src != "web.pdf" {
		t.Errorf("metadata = %v", got[0].Metadata)
	}
}

func TestCloseNil(t *testing.T) {
	var s Store
	if err := s.Close(); err != nil {
		t.Errorf("Close on zero Store: %v", err)
	}
}

func TestNew_ConnectRefused(t *testing.T) {
	if _, err := New("127.0.0.1:1"); err == nil {
		t.Fatal("expected connection error")
	}
}

func TestDelete_Empty(t *testing.T) {
	s := NewWithClient(goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:1"}))
	// No ids: must be a no-op that never touches the network.
	if err := s.Delete(context.Background(), "col", nil); err != nil {
		t.Errorf("Delete(nil): %v", err)
	}
}

func TestCompileTimeInterface(t *testing.T) {
	var _ storage.VectorStore = (*Store)(nil)
}

// TestIntegration exercises the full round-trip against a live Redis Stack
// (RediSearch). It is skipped unless REDISVECTOR_ADDR is set, since miniredis
// cannot emulate FT.* commands.
func TestIntegration(t *testing.T) {
	addr := os.Getenv("REDISVECTOR_ADDR")
	if addr == "" {
		t.Skip("set REDISVECTOR_ADDR to run redisvector integration test")
	}
	s, err := New(addr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	col := "chronos_test_col"
	_ = s.Delete(ctx, col, []string{"a", "b"})
	if err = s.CreateCollection(ctx, col, 3); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	embs := []storage.Embedding{
		{ID: "a", Vector: []float32{1, 0, 0}, Content: "alpha", Metadata: map[string]any{"n": "a"}},
		{ID: "b", Vector: []float32{0, 1, 0}, Content: "beta", Metadata: map[string]any{"n": "b"}},
	}
	if err = s.Upsert(ctx, col, embs); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	res, err := s.Search(ctx, col, []float32{1, 0, 0}, 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) == 0 || res[0].ID != "a" {
		t.Fatalf("expected top hit 'a', got %+v", res)
	}
	if reflect.DeepEqual(res[0].Metadata["n"], "a") == false {
		t.Errorf("metadata not round-tripped: %v", res[0].Metadata)
	}
}
