package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/memory"
)

type putMemoryFailStore struct {
	*memory.Store
}

func (p *putMemoryFailStore) PutMemory(ctx context.Context, m *storage.MemoryRecord) error {
	return fmt.Errorf("put memory failed")
}

func TestEvictLargeResult_PutMemoryError(t *testing.T) {
	base := memory.New()
	store := &putMemoryFailStore{Store: base}
	ctx := context.Background()
	large := string(make([]byte, 1500))
	_, err := EvictLargeResult(ctx, store, "sess", "tool", large)
	if err == nil {
		t.Fatal("expected error from PutMemory")
	}
}

func TestEvictLargeResult_MarshalError(t *testing.T) {
	store := memory.New()
	ctx := context.Background()
	ch := make(chan int)
	_, err := EvictLargeResult(ctx, store, "sess", "tool", ch)
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestReadStoredResult_MapValueMarshalsToJSON(t *testing.T) {
	store := memory.New()
	ctx := context.Background()
	_ = store.PutMemory(ctx, &storage.MemoryRecord{
		ID:        "k1",
		AgentID:   "agent1",
		Kind:      "tool_result_evicted",
		Key:       "k1",
		Value:     map[string]any{"nested": 42},
	})

	out, err := ReadStoredResult(ctx, store, "agent1", "k1")
	if err != nil {
		t.Fatalf("ReadStoredResult: %v", err)
	}
	if out == "" {
		t.Fatal("expected JSON fallback string")
	}
}

func TestCompressToolCalls_OnlyNonToolMessages(t *testing.T) {
	msgs := []model.Message{
		{Role: model.RoleUser, Content: "a"},
		{Role: model.RoleAssistant, Content: "b"},
	}
	out := CompressToolCalls(msgs, 1)
	if len(out) != 2 {
		t.Errorf("got %d messages", len(out))
	}
}
