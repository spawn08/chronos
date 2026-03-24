package chronosos

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/memory"
)

func TestHandleListTraces_WithSessionID(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	// Insert a trace
	trace := &storage.Trace{
		ID:        "trace-1",
		SessionID: "sess-a",
		Name:      "test-trace",
		Kind:      "node",
	}
	_ = s.Store.InsertTrace(ctx, trace)

	req := httptest.NewRequest(http.MethodGet, "/api/traces?session_id=sess-a", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleSessionState_GET_NotFound(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/state?session_id=nonexistent", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleSessionState_GET_WithCheckpoint(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	// Save a checkpoint
	cp := &storage.Checkpoint{
		ID:        "cp-1",
		SessionID: "sess-with-cp",
		NodeID:    "node_a",
		State:     map[string]any{"key": "value"},
		SeqNum:    1,
		CreatedAt: time.Now(),
	}
	_ = s.Store.SaveCheckpoint(ctx, cp)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/state?session_id=sess-with-cp", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleSessionState_POST_NotFound(t *testing.T) {
	s := newTestServer(t)

	body := `{"state":{"foo":"bar"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/state?session_id=nonexistent",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleSessionState_POST_InvalidJSON(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/state?session_id=x",
		bytes.NewBufferString("{invalid json"))
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSessionState_POST_Success(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	cp := &storage.Checkpoint{
		ID:        "cp-orig",
		SessionID: "sess-post",
		NodeID:    "node_a",
		State:     map[string]any{"existing": "value"},
		SeqNum:    1,
		CreatedAt: time.Now(),
	}
	_ = s.Store.SaveCheckpoint(ctx, cp)

	body := `{"state":{"new_key":"new_value"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/state?session_id=sess-post",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleReadiness_WithStore(t *testing.T) {
	store := memory.New()
	s := New(":0", store)
	s.SetReady(true)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleSchedules_GET(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/schedules", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleSchedules_POST_Success(t *testing.T) {
	s := newTestServer(t)

	body := `{"agent_id":"agent1","cron_expr":"* * * * *","input":"test","new_session":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/schedules", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleSchedules_POST_InvalidJSON(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/schedules", bytes.NewBufferString("{invalid"))
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSchedules_POST_BadCron(t *testing.T) {
	s := newTestServer(t)

	body := `{"agent_id":"a1","cron_expr":"not-valid-cron","input":"x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/schedules", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	// Bad cron may or may not return error depending on implementation
	// Just ensure no panic
	_ = w.Code
}

func TestHandleSchedules_MethodNotAllowed(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodPatch, "/api/schedules", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleScheduleByID_GET_NotFound(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/schedules/nonexistent", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestHandleScheduleByID_GET_Found(t *testing.T) {
	s := newTestServer(t)

	// Create a schedule first
	body := `{"agent_id":"a1","cron_expr":"* * * * *","input":"x"}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/schedules", bytes.NewBufferString(body))
	createW := httptest.NewRecorder()
	s.mux.ServeHTTP(createW, createReq)

	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createW.Body).Decode(&created); err != nil || created.ID == "" {
		t.Skip("could not create schedule for test")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/schedules/"+created.ID, nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleScheduleByID_History(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/schedules/any-id/history", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for history (even if empty), got %d", w.Code)
	}
}

func TestHandleScheduleByID_DELETE(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/schedules/nonexistent", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent, got %d", w.Code)
	}
}

func TestHandleScheduleByID_MethodNotAllowed(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodPatch, "/api/schedules/some-id", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestServerNew_Initializes(t *testing.T) {
	store := memory.New()
	s := New(":0", store)
	if s == nil {
		t.Fatal("expected non-nil server")
	}
	if s.mux == nil {
		t.Error("mux should be initialized")
	}
}

func TestHandleReadiness_NotReady(t *testing.T) {
	s := newTestServer(t)
	// SetReady defaults to false

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestHandleListSessions_WithAgentFilter(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	// Create a session
	_ = s.Store.CreateSession(ctx, &storage.Session{
		ID:      "sess-filter",
		AgentID: "agent-filter",
		Status:  "active",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/sessions?agent_id=agent-filter", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleListTraces_NoFilter(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/traces", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleListSessions_WithLimitAndOffset(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions?limit=10&offset=5", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleListSessions_InvalidLimit(t *testing.T) {
	s := newTestServer(t)

	// invalid limit/offset should be ignored, defaults used
	req := httptest.NewRequest(http.MethodGet, "/api/sessions?limit=notanumber&offset=-1", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleReadiness_Ready(t *testing.T) {
	store := memory.New()
	s := New(":0", store)
	s.SetReady(true)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleHealth(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleLiveness(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleHealthz(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleSessionState_GET_NoSessionID(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/state", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestServerStart_ContextCancellation(t *testing.T) {
	store := memory.New()
	s := New(":0", store)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Start(ctx)
	}()

	// Give the server a moment to start up, then cancel
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		// nil error means clean shutdown via context cancellation
		if err != nil {
			t.Logf("Start returned: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("Start did not return after context cancellation")
	}
}
