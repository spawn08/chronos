package mongo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spawn08/chronos/storage"
)

func TestNew(t *testing.T) {
	s, err := New("http://localhost", "my-key", "mydb", "myds")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if s == nil {
		t.Fatal("New returned nil")
	}
	if s.baseURL != "http://localhost" {
		t.Errorf("baseURL = %q", s.baseURL)
	}
	if s.database != "mydb" {
		t.Errorf("database = %q", s.database)
	}
	if s.dataSource != "myds" {
		t.Errorf("dataSource = %q", s.dataSource)
	}
}

func TestMigrateAndClose(t *testing.T) {
	s, _ := New("http://localhost", "", "db", "ds")
	if err := s.Migrate(context.Background()); err != nil {
		t.Errorf("Migrate() error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
}

// newTestServer creates an httptest.Server that serves a MongoDB-like Data API.
// insertHandler handles insertOne, findOneHandler handles findOne, findHandler handles find.
func newMongoTestServer(t *testing.T, handler func(action string, body map[string]any) (int, any)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract action from path: /action/<action>
		var action string
		if len(r.URL.Path) > len("/action/") {
			action = r.URL.Path[len("/action/"):]
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		status, resp := handler(action, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestCreateSession(t *testing.T) {
	srv := newMongoTestServer(t, func(action string, body map[string]any) (int, any) {
		if action != "insertOne" {
			t.Errorf("expected insertOne, got %s", action)
		}
		return http.StatusOK, map[string]any{"insertedId": "s1"}
	})
	defer srv.Close()

	s, _ := New(srv.URL, "", "db", "ds")
	err := s.CreateSession(context.Background(), &storage.Session{ID: "s1", AgentID: "a1", Status: "running"})
	if err != nil {
		t.Errorf("CreateSession() error: %v", err)
	}
}

func TestGetSession(t *testing.T) {
	sess := &storage.Session{ID: "s1", AgentID: "a1", Status: "running"}
	srv := newMongoTestServer(t, func(action string, body map[string]any) (int, any) {
		sessData, _ := json.Marshal(sess)
		var sessMap map[string]any
		json.Unmarshal(sessData, &sessMap)
		return http.StatusOK, map[string]any{"document": sessMap}
	})
	defer srv.Close()

	s, _ := New(srv.URL, "", "db", "ds")
	got, err := s.GetSession(context.Background(), "s1")
	if err != nil {
		t.Fatalf("GetSession() error: %v", err)
	}
	if got.ID != "s1" {
		t.Errorf("ID = %q, want %q", got.ID, "s1")
	}
}

func TestGetSession_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	s, _ := New(srv.URL, "", "db", "ds")
	_, err := s.GetSession(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for invalid JSON response, got nil")
	}
}

func TestListSessions(t *testing.T) {
	sessions := []*storage.Session{
		{ID: "s1", AgentID: "a1", Status: "running"},
		{ID: "s2", AgentID: "a1", Status: "completed"},
	}
	srv := newMongoTestServer(t, func(action string, body map[string]any) (int, any) {
		data, _ := json.Marshal(sessions)
		var docs []map[string]any
		json.Unmarshal(data, &docs)
		return http.StatusOK, map[string]any{"documents": docs}
	})
	defer srv.Close()

	s, _ := New(srv.URL, "", "db", "ds")
	got, err := s.ListSessions(context.Background(), "a1", 10, 0)
	if err != nil {
		t.Fatalf("ListSessions() error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(got))
	}
}

func TestPutMemory(t *testing.T) {
	callCount := 0
	srv := newMongoTestServer(t, func(action string, body map[string]any) (int, any) {
		callCount++
		return http.StatusOK, map[string]any{"deletedCount": 1, "insertedId": "m1"}
	})
	defer srv.Close()

	s, _ := New(srv.URL, "", "db", "ds")
	err := s.PutMemory(context.Background(), &storage.MemoryRecord{ID: "m1", AgentID: "a1", Key: "k", Kind: "long_term"})
	if err != nil {
		t.Errorf("PutMemory() error: %v", err)
	}
	// Should call deleteOne then insertOne
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls (deleteOne+insertOne), got %d", callCount)
	}
}

func TestDoRequest_SetsAPIKey(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("api-key")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	s, _ := New(srv.URL, "test-key", "db", "ds")
	s.do(context.Background(), "find", "sessions", map[string]any{})
	if gotKey != "test-key" {
		t.Errorf("api-key header = %q, want %q", gotKey, "test-key")
	}
}

func newMongoOKServer(t *testing.T) (*httptest.Server, *Store) {
	t.Helper()
	srv := newMongoTestServer(t, func(action string, body map[string]any) (int, any) {
		switch action {
		case "findOne":
			return http.StatusOK, map[string]any{"document": map[string]any{}}
		case "find":
			return http.StatusOK, map[string]any{"documents": []any{}}
		default:
			return http.StatusOK, map[string]any{"insertedId": "ok", "deletedCount": 1}
		}
	})
	t.Cleanup(srv.Close)
	s, _ := New(srv.URL, "", "db", "ds")
	return srv, s
}

func TestUpdateSession_Success(t *testing.T) {
	_, s := newMongoOKServer(t)
	if err := s.UpdateSession(context.Background(), &storage.Session{ID: "s1"}); err != nil {
		t.Errorf("UpdateSession: %v", err)
	}
}

func TestGetMemory_Success(t *testing.T) {
	_, s := newMongoOKServer(t)
	m, err := s.GetMemory(context.Background(), "a1", "key")
	if err != nil {
		t.Errorf("GetMemory: %v", err)
	}
	if m == nil {
		t.Error("expected non-nil")
	}
}

func TestListMemory_Success(t *testing.T) {
	_, s := newMongoOKServer(t)
	mems, err := s.ListMemory(context.Background(), "a1", "episodic")
	if err != nil {
		t.Errorf("ListMemory: %v", err)
	}
	if mems == nil {
		t.Error("expected non-nil slice")
	}
}

func TestDeleteMemory_Success(t *testing.T) {
	_, s := newMongoOKServer(t)
	if err := s.DeleteMemory(context.Background(), "m1"); err != nil {
		t.Errorf("DeleteMemory: %v", err)
	}
}

func TestAppendAuditLog_Success(t *testing.T) {
	_, s := newMongoOKServer(t)
	if err := s.AppendAuditLog(context.Background(), &storage.AuditLog{ID: "l1"}); err != nil {
		t.Errorf("AppendAuditLog: %v", err)
	}
}

func TestListAuditLogs_Success(t *testing.T) {
	_, s := newMongoOKServer(t)
	logs, err := s.ListAuditLogs(context.Background(), "sess", 10, 0)
	if err != nil {
		t.Errorf("ListAuditLogs: %v", err)
	}
	if logs == nil {
		t.Error("expected non-nil slice")
	}
}

func TestInsertTrace_Success(t *testing.T) {
	_, s := newMongoOKServer(t)
	if err := s.InsertTrace(context.Background(), &storage.Trace{ID: "t1"}); err != nil {
		t.Errorf("InsertTrace: %v", err)
	}
}

func TestGetTrace_Success(t *testing.T) {
	_, s := newMongoOKServer(t)
	tr, err := s.GetTrace(context.Background(), "t1")
	if err != nil {
		t.Errorf("GetTrace: %v", err)
	}
	if tr == nil {
		t.Error("expected non-nil trace")
	}
}

func TestListTraces_Success(t *testing.T) {
	_, s := newMongoOKServer(t)
	traces, err := s.ListTraces(context.Background(), "sess")
	if err != nil {
		t.Errorf("ListTraces: %v", err)
	}
	if traces == nil {
		t.Error("expected non-nil slice")
	}
}

func TestAppendEvent_Success(t *testing.T) {
	_, s := newMongoOKServer(t)
	if err := s.AppendEvent(context.Background(), &storage.Event{ID: "e1"}); err != nil {
		t.Errorf("AppendEvent: %v", err)
	}
}

func TestListEvents_Success(t *testing.T) {
	_, s := newMongoOKServer(t)
	events, err := s.ListEvents(context.Background(), "sess", 0)
	if err != nil {
		t.Errorf("ListEvents: %v", err)
	}
	if events == nil {
		t.Error("expected non-nil slice")
	}
}

func TestSaveCheckpoint_Success(t *testing.T) {
	_, s := newMongoOKServer(t)
	if err := s.SaveCheckpoint(context.Background(), &storage.Checkpoint{ID: "cp1"}); err != nil {
		t.Errorf("SaveCheckpoint: %v", err)
	}
}

func TestGetCheckpoint_Success(t *testing.T) {
	_, s := newMongoOKServer(t)
	cp, err := s.GetCheckpoint(context.Background(), "cp1")
	if err != nil {
		t.Errorf("GetCheckpoint: %v", err)
	}
	if cp == nil {
		t.Error("expected non-nil checkpoint")
	}
}

func TestGetLatestCheckpoint_Success(t *testing.T) {
	cp := &storage.Checkpoint{ID: "cp1", SessionID: "sess"}
	srv := newMongoTestServer(t, func(action string, body map[string]any) (int, any) {
		cpData, _ := json.Marshal(cp)
		var cpMap map[string]any
		json.Unmarshal(cpData, &cpMap)
		return http.StatusOK, map[string]any{"documents": []any{cpMap}}
	})
	defer srv.Close()
	s, _ := New(srv.URL, "", "db", "ds")
	got, err := s.GetLatestCheckpoint(context.Background(), "sess")
	if err != nil {
		t.Errorf("GetLatestCheckpoint: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil checkpoint")
	}
}

func TestGetLatestCheckpoint_Empty(t *testing.T) {
	_, s := newMongoOKServer(t)
	_, err := s.GetLatestCheckpoint(context.Background(), "sess")
	if err == nil {
		t.Error("expected error when no checkpoints found")
	}
}

func TestListCheckpoints_Success(t *testing.T) {
	_, s := newMongoOKServer(t)
	cps, err := s.ListCheckpoints(context.Background(), "sess")
	if err != nil {
		t.Errorf("ListCheckpoints: %v", err)
	}
	if cps == nil {
		t.Error("expected non-nil slice")
	}
}
