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

func newTestServer(t *testing.T) *Server {
	t.Helper()
	store := memory.New()
	s := New(":0", store)
	return s
}

func TestHealthEndpoints(t *testing.T) {
	s := newTestServer(t)

	tests := []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{"/healthz", http.StatusOK, `"status"`},
		{"/health", http.StatusOK, `"status"`},
		{"/health/live", http.StatusOK, `"alive"`},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			s.mux.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("path %s: got status %d, want %d", tc.path, w.Code, tc.wantStatus)
			}
			if tc.wantBody != "" && !bytes.Contains(w.Body.Bytes(), []byte(tc.wantBody)) {
				t.Errorf("path %s: body %q does not contain %q", tc.path, w.Body.String(), tc.wantBody)
			}
		})
	}
}

func TestReadinessNotReady(t *testing.T) {
	s := newTestServer(t)
	// ready flag defaults to false

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when not ready, got %d", w.Code)
	}
}

func TestReadinessReady(t *testing.T) {
	s := newTestServer(t)
	s.SetReady(true)

	req := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 when ready, got %d", w.Code)
	}
}

func TestSetReady(t *testing.T) {
	s := newTestServer(t)
	s.SetReady(true)
	if !s.ready.Load() {
		t.Error("expected ready=true")
	}
	s.SetReady(false)
	if s.ready.Load() {
		t.Error("expected ready=false")
	}
}

func TestListSessionsEmpty(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := body["sessions"]; !ok {
		t.Error("response missing 'sessions' key")
	}
}

func TestListSessionsWithData(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	sess := &storage.Session{
		ID:        "sess-1",
		AgentID:   "agent-1",
		Status:    "running",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.Store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions?agent_id=agent-1", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestListSessionsLimitOffset(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions?limit=10&offset=0", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestListTracesNoSessionID(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/traces", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := body["error"]; !ok {
		t.Error("expected error in response for missing session_id")
	}
}

func TestSessionStateMissingID(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/state", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestSessionStateMethodNotAllowed(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/state?session_id=abc", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestSchedulesGetEmpty(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/schedules", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := body["schedules"]; !ok {
		t.Error("expected 'schedules' key in response")
	}
}

func TestSchedulesPostInvalidJSON(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/schedules", bytes.NewBufferString("{invalid"))
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestSchedulesMethodNotAllowed(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/schedules", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestScheduleByIDNotFound(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/schedules/nonexistent", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestScheduleByIDDeleteNotFound(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/schedules/nope", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestScheduleByIDHistory(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/schedules/some-id/history", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := body["history"]; !ok {
		t.Error("expected 'history' key in response")
	}
}

func TestNewServerFields(t *testing.T) {
	store := memory.New()
	s := New(":9090", store)

	if s.Addr != ":9090" {
		t.Errorf("expected Addr :9090, got %s", s.Addr)
	}
	if s.Store == nil {
		t.Error("expected Store to be non-nil")
	}
	if s.Broker == nil {
		t.Error("expected Broker to be non-nil")
	}
	if s.Auth == nil {
		t.Error("expected Auth to be non-nil")
	}
	if s.Trace == nil {
		t.Error("expected Trace to be non-nil")
	}
	if s.Approval == nil {
		t.Error("expected Approval to be non-nil")
	}
	if s.Metrics == nil {
		t.Error("expected Metrics to be non-nil")
	}
	if s.Scheduler == nil {
		t.Error("expected Scheduler to be non-nil")
	}
	if s.mux == nil {
		t.Error("expected mux to be non-nil")
	}
}
