package chronosos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/memory"
)

type listSessionsErrStore struct {
	*memory.Store
}

func (s *listSessionsErrStore) ListSessions(ctx context.Context, agentID string, limit, offset int) ([]*storage.Session, error) {
	return nil, fmt.Errorf("list sessions failed")
}

func TestHandleListSessions_StoreError(t *testing.T) {
	s := New(":0", &listSessionsErrStore{Store: memory.New()})

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", http.NoBody)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want 500", w.Code)
	}
}

type listTracesErrStore struct {
	*memory.Store
}

func (s *listTracesErrStore) ListTraces(ctx context.Context, sessionID string) ([]*storage.Trace, error) {
	return nil, fmt.Errorf("list traces failed")
}

func TestHandleListTraces_StoreError(t *testing.T) {
	s := New(":0", &listTracesErrStore{Store: memory.New()})

	req := httptest.NewRequest(http.MethodGet, "/api/traces?session_id=s1", http.NoBody)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want 500", w.Code)
	}
}

func TestHandleSessionState_GetSuccess(t *testing.T) {
	store := memory.New()
	ctx := context.Background()
	_ = store.CreateSession(ctx, &storage.Session{
		ID: "sess-cp", AgentID: "a1", Status: "running",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	cp := &storage.Checkpoint{
		ID:        "cp1",
		SessionID: "sess-cp",
		RunID:     "r1",
		NodeID:    "n1",
		State:     map[string]any{"k": "v"},
		SeqNum:    1,
		CreatedAt: time.Now(),
	}
	if err := store.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatal(err)
	}

	s := New(":0", store)
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/state?session_id=sess-cp", http.NoBody)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["node_id"] != "n1" {
		t.Errorf("body = %v", body)
	}
}

func TestHandleSessionState_PostUpdateSuccess(t *testing.T) {
	store := memory.New()
	ctx := context.Background()
	_ = store.CreateSession(ctx, &storage.Session{
		ID: "sess-up", AgentID: "a1", Status: "running",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	cp := &storage.Checkpoint{
		ID:        "cp0",
		SessionID: "sess-up",
		RunID:     "r1",
		NodeID:    "n1",
		State:     map[string]any{"x": 1.0},
		SeqNum:    1,
		CreatedAt: time.Now(),
	}
	if err := store.SaveCheckpoint(ctx, cp); err != nil {
		t.Fatal(err)
	}

	s := New(":0", store)
	body := bytes.NewBufferString(`{"state":{"y":2}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/state?session_id=sess-up", body)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleSessionState_PostInvalidJSON(t *testing.T) {
	store := memory.New()
	s := New(":0", store)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/state?session_id=x", bytes.NewBufferString("{"))
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d", w.Code)
	}
}

type migrateErrStore struct {
	*memory.Store
}

func (s *migrateErrStore) Migrate(ctx context.Context) error {
	return fmt.Errorf("migrate failed")
}

func TestHandleReadiness_MigrateFails(t *testing.T) {
	s := New(":0", &migrateErrStore{Store: memory.New()})
	s.SetReady(true)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", http.NoBody)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d, want 503", w.Code)
	}
}

type saveCheckpointErrStore struct {
	*memory.Store
}

func (s *saveCheckpointErrStore) SaveCheckpoint(ctx context.Context, cp *storage.Checkpoint) error {
	return fmt.Errorf("save failed")
}

func TestHandleSessionState_PostSaveCheckpointError(t *testing.T) {
	store := &saveCheckpointErrStore{Store: memory.New()}
	ctx := context.Background()
	_ = store.CreateSession(ctx, &storage.Session{
		ID: "sess-err", AgentID: "a1", Status: "running",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	cp := &storage.Checkpoint{
		ID: "c1", SessionID: "sess-err", RunID: "r", NodeID: "n",
		State: map[string]any{}, SeqNum: 1, CreatedAt: time.Now(),
	}
	_ = store.Store.SaveCheckpoint(ctx, cp)

	s := New(":0", store)
	body := bytes.NewBufferString(`{"state":{"z":1}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/state?session_id=sess-err", body)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want 500", w.Code)
	}
}
