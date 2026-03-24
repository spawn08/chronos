package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/spawn08/chronos/storage"
)

type memStorage struct {
	records map[string]*storage.MemoryRecord
}

func newMemStorage() *memStorage {
	return &memStorage{records: make(map[string]*storage.MemoryRecord)}
}

func (s *memStorage) PutMemory(_ context.Context, m *storage.MemoryRecord) error {
	s.records[m.ID] = m
	return nil
}

func (s *memStorage) GetMemory(_ context.Context, _, key string) (*storage.MemoryRecord, error) {
	for _, m := range s.records {
		if m.Key == key {
			return m, nil
		}
	}
	return nil, errors.New("not found")
}

func (s *memStorage) ListMemory(_ context.Context, agentID, kind string) ([]*storage.MemoryRecord, error) {
	var result []*storage.MemoryRecord
	for _, m := range s.records {
		if m.AgentID == agentID && m.Kind == kind {
			result = append(result, m)
		}
	}
	return result, nil
}

func (s *memStorage) DeleteMemory(_ context.Context, id string) error {
	delete(s.records, id)
	return nil
}

// Unused storage methods - satisfy interface
func (s *memStorage) CreateSession(_ context.Context, _ *storage.Session) error { return nil }
func (s *memStorage) GetSession(_ context.Context, _ string) (*storage.Session, error) {
	return nil, nil
}
func (s *memStorage) UpdateSession(_ context.Context, _ *storage.Session) error { return nil }
func (s *memStorage) ListSessions(_ context.Context, _ string, _, _ int) ([]*storage.Session, error) {
	return nil, nil
}
func (s *memStorage) AppendAuditLog(_ context.Context, _ *storage.AuditLog) error { return nil }
func (s *memStorage) ListAuditLogs(_ context.Context, _ string, _, _ int) ([]*storage.AuditLog, error) {
	return nil, nil
}
func (s *memStorage) InsertTrace(_ context.Context, _ *storage.Trace) error        { return nil }
func (s *memStorage) GetTrace(_ context.Context, _ string) (*storage.Trace, error) { return nil, nil }
func (s *memStorage) ListTraces(_ context.Context, _ string) ([]*storage.Trace, error) {
	return nil, nil
}
func (s *memStorage) AppendEvent(_ context.Context, _ *storage.Event) error { return nil }
func (s *memStorage) ListEvents(_ context.Context, _ string, _ int64) ([]*storage.Event, error) {
	return nil, nil
}
func (s *memStorage) SaveCheckpoint(_ context.Context, _ *storage.Checkpoint) error { return nil }
func (s *memStorage) GetCheckpoint(_ context.Context, _ string) (*storage.Checkpoint, error) {
	return nil, nil
}
func (s *memStorage) GetLatestCheckpoint(_ context.Context, _ string) (*storage.Checkpoint, error) {
	return nil, nil
}
func (s *memStorage) ListCheckpoints(_ context.Context, _ string) ([]*storage.Checkpoint, error) {
	return nil, nil
}
func (s *memStorage) Migrate(_ context.Context) error { return nil }
func (s *memStorage) Close() error                    { return nil }

func TestStore_SetAndGetShortTerm(t *testing.T) {
	store := NewStore("agent1", newMemStorage())
	ctx := context.Background()

	if err := store.SetShortTerm(ctx, "sess1", "color", "blue"); err != nil {
		t.Fatalf("SetShortTerm: %v", err)
	}

	val, err := store.Get(ctx, "color")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "blue" {
		t.Errorf("val = %v, want blue", val)
	}
}

func TestStore_SetAndGetLongTerm(t *testing.T) {
	store := NewStore("agent1", newMemStorage())
	ctx := context.Background()

	if err := store.SetLongTerm(ctx, "pref", "dark-mode"); err != nil {
		t.Fatalf("SetLongTerm: %v", err)
	}

	val, err := store.Get(ctx, "pref")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "dark-mode" {
		t.Errorf("val = %v, want dark-mode", val)
	}
}

func TestStore_GetNotFound(t *testing.T) {
	store := NewStore("agent1", newMemStorage())
	_, err := store.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent key")
	}
}

func TestStore_ListShortTerm(t *testing.T) {
	backend := newMemStorage()
	store := NewStore("agent1", backend)
	ctx := context.Background()

	_ = store.SetShortTerm(ctx, "s1", "key1", "val1")
	_ = store.SetShortTerm(ctx, "s1", "key2", "val2")
	_ = store.SetLongTerm(ctx, "lt1", "val3")

	records, err := store.ListShortTerm(ctx)
	if err != nil {
		t.Fatalf("ListShortTerm: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 short-term records, got %d", len(records))
	}
}

func TestStore_ListLongTerm(t *testing.T) {
	backend := newMemStorage()
	store := NewStore("agent1", backend)
	ctx := context.Background()

	_ = store.SetShortTerm(ctx, "s1", "key1", "val1")
	_ = store.SetLongTerm(ctx, "lt1", "val1")
	_ = store.SetLongTerm(ctx, "lt2", "val2")

	records, err := store.ListLongTerm(ctx)
	if err != nil {
		t.Fatalf("ListLongTerm: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("expected 2 long-term records, got %d", len(records))
	}
}

func TestStore_OverwriteKey(t *testing.T) {
	store := NewStore("agent1", newMemStorage())
	ctx := context.Background()

	_ = store.SetLongTerm(ctx, "key", "first")
	_ = store.SetLongTerm(ctx, "key", "second")

	val, err := store.Get(ctx, "key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "second" {
		t.Errorf("val = %v, want second (overwrite)", val)
	}
}

func TestStore_DifferentAgents(t *testing.T) {
	backend := newMemStorage()
	store1 := NewStore("agent1", backend)
	store2 := NewStore("agent2", backend)
	ctx := context.Background()

	_ = store1.SetLongTerm(ctx, "shared-key", "from-agent1")
	_ = store2.SetLongTerm(ctx, "shared-key", "from-agent2")

	lt1, _ := store1.ListLongTerm(ctx)
	lt2, _ := store2.ListLongTerm(ctx)

	if len(lt1) != 1 {
		t.Errorf("agent1 long-term: expected 1, got %d", len(lt1))
	}
	if len(lt2) != 1 {
		t.Errorf("agent2 long-term: expected 1, got %d", len(lt2))
	}
}
