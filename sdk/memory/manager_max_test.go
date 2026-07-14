package memory

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/spawn08/chronos/storage"
)

type failPutBackend struct {
	*memStorage
}

func (f *failPutBackend) PutMemory(_ context.Context, _ *storage.MemoryRecord) error {
	return errors.New("put blocked")
}

type failListBackend struct {
	*memStorage
}

func (f *failListBackend) ListMemory(_ context.Context, _, _ string) ([]*storage.MemoryRecord, error) {
	return nil, errors.New("list blocked")
}

func TestManager_ExtractMemories_SetLongTermError_Max(t *testing.T) {
	base := newMemStorage()
	backend := &failPutBackend{memStorage: base}
	store := NewStoreForUser("agent1", "u1", backend)
	mgr := NewManager("agent1", "u1", store, &mockProvider{
		response: `[{"key":"k","value":"v"}]`,
	})
	if err := mgr.ExtractMemories(context.Background(), nil); err == nil {
		t.Fatal("expected error from PutMemory")
	}
}

func TestManager_OptimizeMemories_ProviderError_Max(t *testing.T) {
	backend := newMemStorage()
	ctx := context.Background()
	store := NewStoreForUser("agent1", "u1", backend)
	for i := 0; i < 5; i++ {
		// Seed via the tenant-scoped store so keys are namespaced to "u1" and
		// visible to the u1-scoped manager below.
		_ = store.SetLongTerm(ctx, fmt.Sprintf("k%d", i), i)
	}
	mgr := NewManager("agent1", "u1", store, &mockProvider{err: errors.New("boom")})
	if err := mgr.OptimizeMemories(ctx); err == nil {
		t.Fatal("expected provider error")
	}
}

func TestMemoryTools_Recall_ListError_Max(t *testing.T) {
	backend := &failListBackend{memStorage: newMemStorage()}
	store := NewStoreForUser("agent1", "u1", backend)
	mgr := NewManager("agent1", "u1", store, &mockProvider{})
	var recall MemoryTool
	for _, mt := range mgr.MemoryTools() {
		if mt.Name == "recall" {
			recall = mt
			break
		}
	}
	if recall.Name == "" {
		t.Fatal("recall tool not found")
	}
	_, err := recall.Handler(context.Background(), nil)
	if err == nil {
		t.Fatal("expected list error")
	}
}
