package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/spawn08/chronos/storage"
)

// errListStorage fails ListMemory for long_term records.
type errListStorage struct {
	*memStorage
}

func (e *errListStorage) ListMemory(ctx context.Context, agentID, kind string) ([]*storage.MemoryRecord, error) {
	if kind == "long_term" {
		return nil, errors.New("list long_term failed")
	}
	return e.memStorage.ListMemory(ctx, agentID, kind)
}

func TestManager_OptimizeMemories_ListError(t *testing.T) {
	backend := &errListStorage{memStorage: newMemStorage()}
	store := NewStore("agent1", backend)
	mgr := NewManager("agent1", "u1", store, &mockProvider{response: `[]`})

	ctx := context.Background()
	for i := 0; i < 6; i++ {
		_ = store.SetLongTerm(ctx, "k"+string(rune('a'+i)), "v")
	}

	err := mgr.OptimizeMemories(ctx)
	if err == nil {
		t.Fatal("expected error from ListLongTerm")
	}
}

func TestManager_GetUserMemories_ListError(t *testing.T) {
	backend := &errListStorage{memStorage: newMemStorage()}
	store := NewStore("agent1", backend)
	mgr := NewManager("agent1", "u1", store, &mockProvider{})

	_, err := mgr.GetUserMemories(context.Background())
	if err == nil {
		t.Fatal("expected error from ListLongTerm")
	}
}
