package redisvector

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/spawn08/chronos/storage"
)

// miniRediSearch is an in-process fake RediSearch server for testing.
// It handles FT.CREATE, HSET, FT.SEARCH, DEL commands via RESP protocol.
type miniRediSearch struct {
	mu   sync.Mutex
	data map[string]map[string]string // key -> field -> value
	ln   net.Listener
}

func newMiniRediSearch(t *testing.T) (*miniRediSearch, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	mr := &miniRediSearch{
		data: make(map[string]map[string]string),
		ln:   ln,
	}
	go mr.serve()
	return mr, ln.Addr().String()
}

func (mr *miniRediSearch) close() { mr.ln.Close() }

func (mr *miniRediSearch) serve() {
	for {
		conn, err := mr.ln.Accept()
		if err != nil {
			return
		}
		go mr.handleConn(conn)
	}
}

func (mr *miniRediSearch) handleConn(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 65536)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		args := parseRespArgs(string(buf[:n]))
		if len(args) == 0 {
			continue
		}

		mr.mu.Lock()
		resp := mr.handle(args)
		mr.mu.Unlock()

		conn.Write([]byte(resp))
	}
}

func (mr *miniRediSearch) handle(args []string) string {
	if len(args) == 0 {
		return "-ERR empty command\r\n"
	}
	cmd := strings.ToUpper(args[0])
	switch cmd {
	case "FT.CREATE":
		return "+OK\r\n"
	case "HSET":
		if len(args) < 2 {
			return "-ERR\r\n"
		}
		key := args[1]
		if mr.data[key] == nil {
			mr.data[key] = make(map[string]string)
		}
		for i := 2; i+1 < len(args); i += 2 {
			mr.data[key][args[i]] = args[i+1]
		}
		return fmt.Sprintf(":%d\r\n", (len(args)-2)/2)
	case "DEL":
		for _, k := range args[1:] {
			delete(mr.data, k)
		}
		return fmt.Sprintf(":%d\r\n", len(args)-1)
	case "FT.SEARCH":
		// Return empty results
		return "*1\r\n:0\r\n"
	default:
		return "-ERR unknown command\r\n"
	}
}

func parseRespArgs(raw string) []string {
	lines := strings.Split(raw, "\r\n")
	var args []string
	i := 0
	if i >= len(lines) || len(lines[i]) == 0 || lines[i][0] != '*' {
		return args
	}
	count := 0
	fmt.Sscanf(lines[i][1:], "%d", &count)
	i++
	for j := 0; j < count && i < len(lines); j++ {
		if i >= len(lines) || len(lines[i]) == 0 || lines[i][0] != '$' {
			i++
			continue
		}
		i++
		if i < len(lines) {
			args = append(args, lines[i])
			i++
		}
	}
	return args
}

// ---------------------------------------------------------------------------
// Store tests
// ---------------------------------------------------------------------------

func TestStore_CreateCollection(t *testing.T) {
	mr, addr := newMiniRediSearch(t)
	defer mr.close()

	store, err := New(addr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer store.Close()

	if err := store.CreateCollection(context.Background(), "test_col", 128); err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
}

func TestStore_Upsert(t *testing.T) {
	mr, addr := newMiniRediSearch(t)
	defer mr.close()

	store, _ := New(addr)
	defer store.Close()

	embeddings := []storage.Embedding{
		{
			ID:      "doc1",
			Vector:  []float32{0.1, 0.2, 0.3},
			Content: "hello world",
			Metadata: map[string]any{
				"source": "test",
			},
		},
		{
			ID:      "doc2",
			Vector:  []float32{0.4, 0.5, 0.6},
			Content: "second doc",
		},
	}

	if err := store.Upsert(context.Background(), "test_col", embeddings); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
}

func TestStore_Search_EmptyResults(t *testing.T) {
	mr, addr := newMiniRediSearch(t)
	defer mr.close()

	store, _ := New(addr)
	defer store.Close()

	results, err := store.Search(context.Background(), "test_col", []float32{0.1, 0.2, 0.3}, 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// Our miniRedis always returns empty results for FT.SEARCH
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestStore_Delete(t *testing.T) {
	mr, addr := newMiniRediSearch(t)
	defer mr.close()

	store, _ := New(addr)
	defer store.Close()

	// First upsert
	store.Upsert(context.Background(), "col", []storage.Embedding{
		{ID: "d1", Vector: []float32{0.1}, Content: "test"},
	})

	// Delete
	if err := store.Delete(context.Background(), "col", []string{"d1"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestStore_Delete_Multiple(t *testing.T) {
	mr, addr := newMiniRediSearch(t)
	defer mr.close()

	store, _ := New(addr)
	defer store.Close()

	store.Upsert(context.Background(), "col", []storage.Embedding{
		{ID: "a", Vector: []float32{0.1}, Content: "a"},
		{ID: "b", Vector: []float32{0.2}, Content: "b"},
		{ID: "c", Vector: []float32{0.3}, Content: "c"},
	})

	if err := store.Delete(context.Background(), "col", []string{"a", "b", "c"}); err != nil {
		t.Fatalf("Delete multiple: %v", err)
	}
}

func TestNew_ConnectionFailed(t *testing.T) {
	_, err := New("127.0.0.1:19998")
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestStore_Close(t *testing.T) {
	mr, addr := newMiniRediSearch(t)
	defer mr.close()

	store, err := New(addr)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestFloatsToString_EdgeCases(t *testing.T) {
	tests := []struct {
		input []float32
		want  string
	}{
		{nil, ""},
		{[]float32{}, ""},
		{[]float32{1.0}, "1"},
		{[]float32{0.5, 0.5}, "0.5,0.5"},
	}

	for _, tt := range tests {
		got := floatsToString(tt.input)
		if got != tt.want {
			t.Errorf("floatsToString(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
