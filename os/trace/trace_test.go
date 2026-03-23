package trace

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/spawn08/chronos/storage"
)

// traceTestStorage implements storage.Storage with in-memory trace + audit log persistence.
type traceTestStorage struct {
	mu        sync.Mutex
	traces    []*storage.Trace
	auditLogs []*storage.AuditLog
	failNext  bool
}

func newTraceTestStorage() *traceTestStorage {
	return &traceTestStorage{
		traces:    make([]*storage.Trace, 0),
		auditLogs: make([]*storage.AuditLog, 0),
	}
}

func (s *traceTestStorage) InsertTrace(_ context.Context, t *storage.Trace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext {
		s.failNext = false
		return errors.New("storage failure")
	}
	s.traces = append(s.traces, t)
	return nil
}

func (s *traceTestStorage) GetTrace(_ context.Context, id string) (*storage.Trace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.traces {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, errors.New("not found")
}

func (s *traceTestStorage) ListTraces(_ context.Context, _ string) ([]*storage.Trace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]*storage.Trace, len(s.traces))
	copy(result, s.traces)
	return result, nil
}

func (s *traceTestStorage) AppendAuditLog(_ context.Context, log *storage.AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext {
		s.failNext = false
		return errors.New("storage failure")
	}
	s.auditLogs = append(s.auditLogs, log)
	return nil
}

func (s *traceTestStorage) ListAuditLogs(_ context.Context, _ string, _, _ int) ([]*storage.AuditLog, error) {
	return nil, nil
}

// Stubs for remaining Storage interface methods
func (s *traceTestStorage) CreateSession(_ context.Context, _ *storage.Session) error { return nil }
func (s *traceTestStorage) GetSession(_ context.Context, _ string) (*storage.Session, error) {
	return nil, errors.New("not implemented")
}
func (s *traceTestStorage) UpdateSession(_ context.Context, _ *storage.Session) error { return nil }
func (s *traceTestStorage) ListSessions(_ context.Context, _ string, _, _ int) ([]*storage.Session, error) {
	return nil, nil
}
func (s *traceTestStorage) AppendEvent(_ context.Context, _ *storage.Event) error { return nil }
func (s *traceTestStorage) ListEvents(_ context.Context, _ string, _ int64) ([]*storage.Event, error) {
	return nil, nil
}
func (s *traceTestStorage) SaveCheckpoint(_ context.Context, _ *storage.Checkpoint) error {
	return nil
}
func (s *traceTestStorage) GetCheckpoint(_ context.Context, _ string) (*storage.Checkpoint, error) {
	return nil, errors.New("not implemented")
}
func (s *traceTestStorage) GetLatestCheckpoint(_ context.Context, _ string) (*storage.Checkpoint, error) {
	return nil, errors.New("not implemented")
}
func (s *traceTestStorage) ListCheckpoints(_ context.Context, _ string) ([]*storage.Checkpoint, error) {
	return nil, nil
}
func (s *traceTestStorage) PutMemory(_ context.Context, _ *storage.MemoryRecord) error { return nil }
func (s *traceTestStorage) GetMemory(_ context.Context, _, _ string) (*storage.MemoryRecord, error) {
	return nil, errors.New("not implemented")
}
func (s *traceTestStorage) ListMemory(_ context.Context, _, _ string) ([]*storage.MemoryRecord, error) {
	return nil, nil
}
func (s *traceTestStorage) DeleteMemory(_ context.Context, _ string) error { return nil }
func (s *traceTestStorage) Migrate(_ context.Context) error                { return nil }
func (s *traceTestStorage) Close() error                                   { return nil }

// --- Tests ---

func TestNewCollector(t *testing.T) {
	store := newTraceTestStorage()
	c := NewCollector(store)
	if c == nil {
		t.Fatal("NewCollector returned nil")
	}
}

func TestStartSpan(t *testing.T) {
	store := newTraceTestStorage()
	c := NewCollector(store)
	ctx := context.Background()

	span, err := c.StartSpan(ctx, "session-1", "graph:main", "graph")
	if err != nil {
		t.Fatalf("StartSpan: %v", err)
	}

	if span == nil {
		t.Fatal("StartSpan returned nil span")
	}
	if span.SessionID != "session-1" {
		t.Errorf("SessionID = %q, want %q", span.SessionID, "session-1")
	}
	if span.Name != "graph:main" {
		t.Errorf("Name = %q, want %q", span.Name, "graph:main")
	}
	if span.Kind != "graph" {
		t.Errorf("Kind = %q, want %q", span.Kind, "graph")
	}
	if span.StartedAt.IsZero() {
		t.Error("StartedAt should not be zero")
	}
	if span.ID == "" {
		t.Error("ID should not be empty")
	}

	store.mu.Lock()
	if len(store.traces) != 1 {
		t.Errorf("expected 1 trace in storage, got %d", len(store.traces))
	}
	store.mu.Unlock()
}

func TestEndSpan(t *testing.T) {
	store := newTraceTestStorage()
	c := NewCollector(store)
	ctx := context.Background()

	span, err := c.StartSpan(ctx, "s1", "node:step1", "node")
	if err != nil {
		t.Fatalf("StartSpan: %v", err)
	}

	output := map[string]any{"result": "done"}
	err = c.EndSpan(ctx, span, output, "")
	if err != nil {
		t.Fatalf("EndSpan: %v", err)
	}

	if span.EndedAt.IsZero() {
		t.Error("EndedAt should be set after EndSpan")
	}
	if span.Output == nil {
		t.Error("Output should be set after EndSpan")
	}
	if span.Error != "" {
		t.Errorf("Error should be empty, got %q", span.Error)
	}

	store.mu.Lock()
	if len(store.traces) != 2 {
		t.Errorf("expected 2 trace inserts (start + end), got %d", len(store.traces))
	}
	store.mu.Unlock()
}

func TestEndSpan_WithError(t *testing.T) {
	store := newTraceTestStorage()
	c := NewCollector(store)
	ctx := context.Background()

	span, _ := c.StartSpan(ctx, "s-err", "node:fail", "node")

	err := c.EndSpan(ctx, span, nil, "something went wrong")
	if err != nil {
		t.Fatalf("EndSpan: %v", err)
	}

	if span.Error != "something went wrong" {
		t.Errorf("Error = %q, want %q", span.Error, "something went wrong")
	}
}

func TestAudit(t *testing.T) {
	store := newTraceTestStorage()
	c := NewCollector(store)
	ctx := context.Background()

	err := c.Audit(ctx, "session-1", "user-42", "tool_execute", "weather_api")
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	if len(store.auditLogs) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(store.auditLogs))
	}

	log := store.auditLogs[0]
	if log.SessionID != "session-1" {
		t.Errorf("SessionID = %q, want %q", log.SessionID, "session-1")
	}
	if log.Actor != "user-42" {
		t.Errorf("Actor = %q, want %q", log.Actor, "user-42")
	}
	if log.Action != "tool_execute" {
		t.Errorf("Action = %q, want %q", log.Action, "tool_execute")
	}
	if log.Resource != "weather_api" {
		t.Errorf("Resource = %q, want %q", log.Resource, "weather_api")
	}
	if log.ID == "" {
		t.Error("audit log ID should not be empty")
	}
	if log.CreatedAt.IsZero() {
		t.Error("audit log CreatedAt should not be zero")
	}
}

func TestStartSpan_UniqueIDs(t *testing.T) {
	store := newTraceTestStorage()
	c := NewCollector(store)
	ctx := context.Background()

	ids := make(map[string]bool)
	for i := 0; i < 20; i++ {
		span, err := c.StartSpan(ctx, "s", "span", "test")
		if err != nil {
			t.Fatalf("StartSpan[%d]: %v", i, err)
		}
		if ids[span.ID] {
			t.Fatalf("duplicate span ID: %q", span.ID)
		}
		ids[span.ID] = true
		time.Sleep(time.Microsecond * 10)
	}
}

func TestStartSpan_StorageFailure(t *testing.T) {
	store := newTraceTestStorage()
	store.failNext = true
	c := NewCollector(store)

	_, err := c.StartSpan(context.Background(), "s", "name", "kind")
	if err == nil {
		t.Fatal("expected error when storage fails")
	}
}

func TestEndSpan_StorageFailure(t *testing.T) {
	store := newTraceTestStorage()
	c := NewCollector(store)
	ctx := context.Background()

	span, _ := c.StartSpan(ctx, "s", "name", "kind")

	store.mu.Lock()
	store.failNext = true
	store.mu.Unlock()

	err := c.EndSpan(ctx, span, nil, "")
	if err == nil {
		t.Fatal("expected error when storage fails on EndSpan")
	}
}

func TestAudit_StorageFailure(t *testing.T) {
	store := newTraceTestStorage()
	store.failNext = true
	c := NewCollector(store)

	err := c.Audit(context.Background(), "s", "actor", "action", "resource")
	if err == nil {
		t.Fatal("expected error when storage fails on Audit")
	}
}

func TestSpanTimingOrder(t *testing.T) {
	store := newTraceTestStorage()
	c := NewCollector(store)
	ctx := context.Background()

	span, _ := c.StartSpan(ctx, "s", "op", "test")
	startTime := span.StartedAt

	time.Sleep(time.Millisecond)
	_ = c.EndSpan(ctx, span, nil, "")

	if !span.EndedAt.After(startTime) {
		t.Errorf("EndedAt (%v) should be after StartedAt (%v)", span.EndedAt, startTime)
	}
}

func TestConcurrentSpans(t *testing.T) {
	store := newTraceTestStorage()
	c := NewCollector(store)
	ctx := context.Background()

	var wg sync.WaitGroup
	const goroutines = 20

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			span, err := c.StartSpan(ctx, "s-conc", "span", "test")
			if err != nil {
				return
			}
			_ = c.EndSpan(ctx, span, map[string]any{"idx": idx}, "")
		}(i)
	}

	wg.Wait()

	store.mu.Lock()
	traceCount := len(store.traces)
	store.mu.Unlock()

	// Each goroutine inserts 2 traces (start + end)
	expected := goroutines * 2
	if traceCount != expected {
		t.Errorf("expected %d trace inserts from %d goroutines, got %d", expected, goroutines, traceCount)
	}
}
