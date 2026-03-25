package redis

import (
	"context"
	"strings"
	"testing"
)

func TestSet_MarshalError_ITER6(t *testing.T) {
	mr, addr := newMiniRedis(t)
	defer mr.close()

	s, err := New(addr, "", 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	err = s.set(ctx, "bad-key", make(chan int))
	if err == nil || !strings.Contains(err.Error(), "marshal") {
		t.Fatalf("expected marshal error, got %v", err)
	}
}

func TestGet_InvalidJSON_ITER6(t *testing.T) {
	mr, addr := newMiniRedis(t)
	defer mr.close()

	s, err := New(addr, "", 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	s.mu.Lock()
	_, _ = s.rawCmdResp("SET", "k1", `not-json{`)
	s.mu.Unlock()

	var out map[string]any
	err = s.get(ctx, "k1", &out)
	if err == nil || !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("expected unmarshal error, got %v", err)
	}
}

func TestNew_ConnectRefused_ITER6(t *testing.T) {
	_, err := New("127.0.0.1:1", "", 0)
	if err == nil {
		t.Fatal("expected connect error")
	}
}
