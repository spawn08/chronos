package redisvector

import (
	"context"
	"net"
	"testing"

	"github.com/spawn08/chronos/storage"
)

func TestParseSearchResponse_SkipsWrongPrefix_Boost(t *testing.T) {
	resp := "*3\r\n" +
		":1\r\n" +
		"$12\r\nothercol:x\r\n" +
		"*2\r\n" +
		"$7\r\ncontent\r\n" +
		"$1\r\nz\r\n"
	results := ParseSearchResponse(resp, "mycol")
	if len(results) != 0 {
		t.Fatalf("expected no results when key prefix mismatches, got %d", len(results))
	}
}

func TestParseSearchResponse_ShortResponse_Boost(t *testing.T) {
	if got := ParseSearchResponse("x", "c"); got != nil {
		t.Errorf("want nil for single-line response, got %#v", got)
	}
}

func TestParseSearchResponse_MalformedFieldArray_Boost(t *testing.T) {
	// Bulk key then '*' with bad element count / early break in inner loop
	resp := "*3\r\n" +
		":1\r\n" +
		"$9\r\nmycol:id1\r\n" +
		"*-1\r\n"
	results := ParseSearchResponse(resp, "mycol")
	if len(results) == 0 {
		t.Fatal("expected one skeletal result from doc key parse")
	}
	if results[0].ID != "id1" {
		t.Errorf("ID = %q", results[0].ID)
	}
}

func TestStore_Close_NilConn_Boost(t *testing.T) {
	var s Store
	if err := s.Close(); err != nil {
		t.Errorf("Close on zero Store: %v", err)
	}
}

func TestStore_Search_WriteFails_Boost(t *testing.T) {
	c1, c2 := net.Pipe()
	_ = c2.Close()
	s := &Store{addr: "pipe", conn: c1}
	defer c1.Close()

	_, err := s.Search(context.Background(), "col", []float32{0.1, 0.2}, 2)
	if err == nil {
		t.Fatal("expected error when connection is broken")
	}
}

func TestStore_rawCmd_ErrorPrefix_Boost(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()

	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 4096)
		_, _ = c.Read(buf)
		_, _ = c.Write([]byte("-ERR nope\r\n"))
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	s := &Store{addr: ln.Addr().String(), conn: conn}
	_, err = s.rawCmd("PING")
	if err == nil || err.Error() == "" {
		t.Fatalf("expected redisvector error response, got %v", err)
	}
}

func TestUpsert_UsesFloatsToString_Boost(t *testing.T) {
	// Indirect coverage: Upsert builds vector string via floatsToString
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		for {
			buf := make([]byte, 65536)
			n, err := c.Read(buf)
			if err != nil || n == 0 {
				return
			}
			_, _ = c.Write([]byte("+OK\r\n"))
		}
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	s := &Store{addr: ln.Addr().String(), conn: conn}
	t.Cleanup(func() { _ = s.Close(); <-done })

	err = s.Upsert(context.Background(), "c", []storage.Embedding{
		{ID: "1", Vector: []float32{1, 2.5, 3}, Content: "hi", Metadata: map[string]any{"k": "v"}},
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
}
