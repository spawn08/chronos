package dynamo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spawn08/chronos/storage"
)

func TestNew(t *testing.T) {
	s, err := New("http://localhost:8000", "chronos", "us-east-1", "key", "secret")
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if s == nil {
		t.Fatal("New returned nil")
	}
	if s.endpoint != "http://localhost:8000" {
		t.Errorf("endpoint = %q", s.endpoint)
	}
	if s.tableName != "chronos" {
		t.Errorf("tableName = %q", s.tableName)
	}
	if s.region != "us-east-1" {
		t.Errorf("region = %q", s.region)
	}
}

func TestClose(t *testing.T) {
	s, _ := New("http://localhost:8000", "t", "r", "k", "sk")
	if err := s.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
}

func TestMarshalItem(t *testing.T) {
	tests := []struct {
		name string
		v    any
		keys []string
	}{
		{
			name: "string fields",
			v:    map[string]any{"id": "123", "name": "test"},
			keys: []string{"id", "name"},
		},
		{
			name: "numeric field",
			v:    map[string]any{"count": float64(42)},
			keys: []string{"count"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := marshalItem(tt.v)
			for _, k := range tt.keys {
				if _, ok := item[k]; !ok {
					t.Errorf("marshalItem missing key %q", k)
				}
			}
		})
	}
}

func TestMarshalItem_StringField(t *testing.T) {
	item := marshalItem(map[string]any{"id": "abc"})
	val, ok := item["id"]
	if !ok {
		t.Fatal("missing 'id' key")
	}
	m, ok := val.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string, got %T", val)
	}
	if m["S"] != "abc" {
		t.Errorf("S = %q, want %q", m["S"], "abc")
	}
}

func TestMarshalItem_NumericField(t *testing.T) {
	item := marshalItem(map[string]any{"count": float64(42)})
	val := item["count"]
	m, ok := val.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string, got %T", val)
	}
	if m["N"] != "42" {
		t.Errorf("N = %q, want %q", m["N"], "42")
	}
}

func TestMarshalItem_ComplexField(t *testing.T) {
	nested := map[string]any{"key": "value"}
	item := marshalItem(map[string]any{"data": nested})
	val := item["data"]
	m, ok := val.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string, got %T", val)
	}
	// Complex types are JSON-encoded as S
	var decoded map[string]any
	if err := json.Unmarshal([]byte(m["S"]), &decoded); err != nil {
		t.Errorf("failed to decode complex field: %v", err)
	}
}

func TestDoRequest_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"__type":"ValidationException","message":"bad input"}`))
	}))
	defer srv.Close()

	s, _ := New(srv.URL, "table", "us-east-1", "key", "secret")
	_, err := s.doRequest(context.Background(), "PutItem", map[string]any{"test": "data"})
	if err == nil {
		t.Fatal("expected error for HTTP 400, got nil")
	}
}

func TestDoRequest_CorrectTarget(t *testing.T) {
	var gotTarget string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTarget = r.Header.Get("X-Amz-Target")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s, _ := New(srv.URL, "table", "us-east-1", "key", "secret")
	s.doRequest(context.Background(), "PutItem", map[string]any{})
	if gotTarget != "DynamoDB_20120810.PutItem" {
		t.Errorf("X-Amz-Target = %q, want %q", gotTarget, "DynamoDB_20120810.PutItem")
	}
}

func TestGetLatestCheckpoint_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Doesn't matter since GetLatestCheckpoint doesn't make HTTP calls
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s, _ := New(srv.URL, "table", "us-east-1", "", "")
	_, err := s.GetLatestCheckpoint(context.Background(), "session-123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func newOKServer(t *testing.T) (*httptest.Server, *Store) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	s, _ := New(srv.URL, "table", "us-east-1", "key", "secret")
	return srv, s
}

func TestPutItem_Success(t *testing.T) {
	_, s := newOKServer(t)
	if err := s.putItem(context.Background(), map[string]any{"id": "1"}); err != nil {
		t.Errorf("putItem: %v", err)
	}
}

func TestCreateSession_Success(t *testing.T) {
	_, s := newOKServer(t)
	if err := s.CreateSession(context.Background(), &storage.Session{ID: "s1"}); err != nil {
		t.Errorf("CreateSession: %v", err)
	}
}

func TestGetSession_Success(t *testing.T) {
	_, s := newOKServer(t)
	sess, err := s.GetSession(context.Background(), "s1")
	if err != nil {
		t.Errorf("GetSession: %v", err)
	}
	if sess == nil {
		t.Error("expected non-nil session")
	}
}

func TestUpdateSession_Success(t *testing.T) {
	_, s := newOKServer(t)
	if err := s.UpdateSession(context.Background(), &storage.Session{ID: "s1"}); err != nil {
		t.Errorf("UpdateSession: %v", err)
	}
}

func TestListSessions_Success(t *testing.T) {
	_, s := newOKServer(t)
	sessions, err := s.ListSessions(context.Background(), "agent1", 10, 0)
	if err != nil {
		t.Errorf("ListSessions: %v", err)
	}
	if sessions == nil {
		t.Error("expected non-nil slice")
	}
}

func TestPutMemory_Success(t *testing.T) {
	_, s := newOKServer(t)
	if err := s.PutMemory(context.Background(), &storage.MemoryRecord{AgentID: "a1", Key: "k"}); err != nil {
		t.Errorf("PutMemory: %v", err)
	}
}

func TestGetMemory_Success(t *testing.T) {
	_, s := newOKServer(t)
	m, err := s.GetMemory(context.Background(), "a1", "key")
	if err != nil {
		t.Errorf("GetMemory: %v", err)
	}
	if m == nil {
		t.Error("expected non-nil")
	}
}

func TestListMemory_Success(t *testing.T) {
	_, s := newOKServer(t)
	records, err := s.ListMemory(context.Background(), "a1", "episodic")
	if err != nil {
		t.Errorf("ListMemory: %v", err)
	}
	if records == nil {
		t.Error("expected non-nil slice")
	}
}

func TestDeleteMemory_Success(t *testing.T) {
	_, s := newOKServer(t)
	if err := s.DeleteMemory(context.Background(), "m1"); err != nil {
		t.Errorf("DeleteMemory: %v", err)
	}
}

func TestAppendAuditLog_Success(t *testing.T) {
	_, s := newOKServer(t)
	if err := s.AppendAuditLog(context.Background(), &storage.AuditLog{ID: "l1"}); err != nil {
		t.Errorf("AppendAuditLog: %v", err)
	}
}

func TestListAuditLogs_Success(t *testing.T) {
	_, s := newOKServer(t)
	logs, err := s.ListAuditLogs(context.Background(), "sess", 10, 0)
	if err != nil {
		t.Errorf("ListAuditLogs: %v", err)
	}
	if logs == nil {
		t.Error("expected non-nil slice")
	}
}

func TestInsertTrace_Success(t *testing.T) {
	_, s := newOKServer(t)
	if err := s.InsertTrace(context.Background(), &storage.Trace{ID: "t1"}); err != nil {
		t.Errorf("InsertTrace: %v", err)
	}
}

func TestGetTrace_Success(t *testing.T) {
	_, s := newOKServer(t)
	tr, err := s.GetTrace(context.Background(), "t1")
	if err != nil {
		t.Errorf("GetTrace: %v", err)
	}
	if tr == nil {
		t.Error("expected non-nil trace")
	}
}

func TestListTraces_Success(t *testing.T) {
	_, s := newOKServer(t)
	traces, err := s.ListTraces(context.Background(), "sess")
	if err != nil {
		t.Errorf("ListTraces: %v", err)
	}
	if traces == nil {
		t.Error("expected non-nil slice")
	}
}

func TestAppendEvent_Success(t *testing.T) {
	_, s := newOKServer(t)
	if err := s.AppendEvent(context.Background(), &storage.Event{ID: "e1"}); err != nil {
		t.Errorf("AppendEvent: %v", err)
	}
}

func TestListEvents_Success(t *testing.T) {
	_, s := newOKServer(t)
	events, err := s.ListEvents(context.Background(), "sess", 0)
	if err != nil {
		t.Errorf("ListEvents: %v", err)
	}
	if events == nil {
		t.Error("expected non-nil slice")
	}
}

func TestSaveCheckpoint_Success(t *testing.T) {
	_, s := newOKServer(t)
	if err := s.SaveCheckpoint(context.Background(), &storage.Checkpoint{ID: "cp1"}); err != nil {
		t.Errorf("SaveCheckpoint: %v", err)
	}
}

func TestGetCheckpoint_Success(t *testing.T) {
	_, s := newOKServer(t)
	cp, err := s.GetCheckpoint(context.Background(), "cp1")
	if err != nil {
		t.Errorf("GetCheckpoint: %v", err)
	}
	if cp == nil {
		t.Error("expected non-nil checkpoint")
	}
}

func TestListCheckpoints_Success(t *testing.T) {
	_, s := newOKServer(t)
	cps, err := s.ListCheckpoints(context.Background(), "sess")
	if err != nil {
		t.Errorf("ListCheckpoints: %v", err)
	}
	if cps == nil {
		t.Error("expected non-nil slice")
	}
}

func TestMigrate_Success(t *testing.T) {
	_, s := newOKServer(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Errorf("Migrate: %v", err)
	}
}

func TestMigrate_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"already exists"}`))
	}))
	defer srv.Close()
	s, _ := New(srv.URL, "table", "us-east-1", "", "")
	if err := s.Migrate(context.Background()); err == nil {
		t.Error("expected error from Migrate on HTTP 400")
	}
}
