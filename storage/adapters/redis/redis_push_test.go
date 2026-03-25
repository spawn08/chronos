package redis

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/spawn08/chronos/storage"
)

func TestStore_New_AuthFailure_Push(t *testing.T) {
	mr, addr := newMiniRedis(t)
	defer mr.close()

	_, err := New(addr, "secret", 0)
	if err == nil {
		t.Fatal("expected auth error (miniRedis does not implement AUTH)")
	}
	if !strings.Contains(err.Error(), "auth") && !strings.Contains(err.Error(), "redis error") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStore_New_SelectDBFailure_Push(t *testing.T) {
	mr, addr := newMiniRedis(t)
	defer mr.close()

	_, err := New(addr, "", 1)
	if err == nil {
		t.Fatal("expected error when SELECT db is sent to miniRedis")
	}
}

func TestStore_CreateSession_MarshalError_Push(t *testing.T) {
	mr, addr := newMiniRedis(t)
	defer mr.close()

	s, err := New(addr, "", 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	bad := &storage.Session{
		ID:        "s1",
		AgentID:   "a1",
		Status:    "running",
		Metadata:  map[string]any{"ch": make(chan int)},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.CreateSession(context.Background(), bad); err == nil || !strings.Contains(err.Error(), "marshal") {
		t.Fatalf("expected marshal error, got %v", err)
	}
}

func TestStore_PutMemory_MarshalError_Push(t *testing.T) {
	mr, addr := newMiniRedis(t)
	defer mr.close()

	s, err := New(addr, "", 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	m := &storage.MemoryRecord{
		ID:        "m1",
		AgentID:   "a1",
		Kind:      "long_term",
		Key:       "k",
		Value:     make(chan int),
		CreatedAt: time.Now(),
	}
	if err := s.PutMemory(context.Background(), m); err == nil || !strings.Contains(err.Error(), "marshal") {
		t.Fatalf("expected marshal error, got %v", err)
	}
}

func TestStore_AppendAuditLog_MarshalError_Push(t *testing.T) {
	mr, addr := newMiniRedis(t)
	defer mr.close()

	s, err := New(addr, "", 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	log := &storage.AuditLog{
		ID:        "l1",
		SessionID: "sess",
		Actor:     "u",
		Action:    "a",
		Resource:  "r",
		Detail:    map[string]any{"x": make(chan int)},
		CreatedAt: time.Now(),
	}
	if err := s.AppendAuditLog(context.Background(), log); err == nil || !strings.Contains(err.Error(), "marshal") {
		t.Fatalf("expected marshal error, got %v", err)
	}
}

func TestStore_InsertTrace_MarshalError_Push(t *testing.T) {
	mr, addr := newMiniRedis(t)
	defer mr.close()

	s, err := New(addr, "", 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	tr := &storage.Trace{
		ID:        "t1",
		SessionID: "sess",
		Name:      "n",
		Kind:      "k",
		Input:     map[string]any{"bad": make(chan int)},
		StartedAt: time.Now(),
	}
	if err := s.InsertTrace(context.Background(), tr); err == nil || !strings.Contains(err.Error(), "marshal") {
		t.Fatalf("expected marshal error, got %v", err)
	}
}

func TestStore_AppendEvent_MarshalError_Push(t *testing.T) {
	mr, addr := newMiniRedis(t)
	defer mr.close()

	s, err := New(addr, "", 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	e := &storage.Event{
		ID:        "e1",
		SessionID: "sess",
		SeqNum:    1,
		Type:      "t",
		Payload:   map[string]any{"bad": make(chan int)},
		CreatedAt: time.Now(),
	}
	if err := s.AppendEvent(context.Background(), e); err == nil || !strings.Contains(err.Error(), "marshal") {
		t.Fatalf("expected marshal error, got %v", err)
	}
}

func TestStore_SaveCheckpoint_MarshalError_Push(t *testing.T) {
	mr, addr := newMiniRedis(t)
	defer mr.close()

	s, err := New(addr, "", 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	cp := &storage.Checkpoint{
		ID:        "cp1",
		SessionID: "sess",
		RunID:     "r1",
		NodeID:    "n1",
		State:     map[string]any{"bad": make(chan int)},
		SeqNum:    1,
		CreatedAt: time.Now(),
	}
	if err := s.SaveCheckpoint(context.Background(), cp); err == nil || !strings.Contains(err.Error(), "marshal") {
		t.Fatalf("expected marshal error, got %v", err)
	}
}

func TestStore_GetMemory_NotFound_Push(t *testing.T) {
	mr, addr := newMiniRedis(t)
	defer mr.close()

	s, err := New(addr, "", 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	_, err = s.GetMemory(context.Background(), "agent", "missing-key")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestStore_GetTrace_NotFound_Push(t *testing.T) {
	mr, addr := newMiniRedis(t)
	defer mr.close()

	s, err := New(addr, "", 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	_, err = s.GetTrace(context.Background(), "no-such-trace")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestStore_GetCheckpoint_NotFound_Push(t *testing.T) {
	mr, addr := newMiniRedis(t)
	defer mr.close()

	s, err := New(addr, "", 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	_, err = s.GetCheckpoint(context.Background(), "no-such-cp")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestStore_Close_NilConn_Push(t *testing.T) {
	var s Store
	if err := s.Close(); err != nil {
		t.Fatalf("Close with nil conn should return nil, got %v", err)
	}
}

func TestStore_Close_AfterClose_Push(t *testing.T) {
	mr, addr := newMiniRedis(t)
	defer mr.close()

	s, err := New(addr, "", 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// Second close may return an error from the OS; we only require no panic.
	_ = s.Close()
}

// errAfterSetRedis accepts one connection and returns +OK for SET, -ERR for ZADD
// so CreateSession fails on the index update path.
func errAfterSetRedis(t *testing.T) (addr string, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 65536)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			req := string(buf[:n])
			switch {
			case strings.Contains(req, "ZREVRANGE"):
				_, _ = conn.Write([]byte("-ERR zrevrange failed\r\n"))
			case strings.Contains(req, "ZADD"):
				_, _ = conn.Write([]byte("-ERR zadd failed\r\n"))
			case strings.Contains(req, "SET"):
				_, _ = conn.Write([]byte("+OK\r\n"))
			default:
				_, _ = conn.Write([]byte("+OK\r\n"))
			}
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

func TestStore_CreateSession_ZADDFails_Push(t *testing.T) {
	addr, cleanup := errAfterSetRedis(t)
	defer cleanup()

	s, err := New(addr, "", 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	now := time.Now()
	sess := &storage.Session{
		ID: "s1", AgentID: "a1", Status: "running",
		CreatedAt: now, UpdatedAt: now,
	}
	err = s.CreateSession(context.Background(), sess)
	if err == nil || !strings.Contains(err.Error(), "redis error") {
		t.Fatalf("expected redis error from ZADD, got %v", err)
	}
}

func TestStore_ListSessions_ZREVRANGEFails_Push(t *testing.T) {
	addr, cleanup := errAfterSetRedis(t)
	defer cleanup()

	s, err := New(addr, "", 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	_, err = s.ListSessions(context.Background(), "agent", 10, 0)
	if err == nil || !strings.Contains(err.Error(), "list sessions") {
		t.Fatalf("expected list sessions error, got %v", err)
	}
}
