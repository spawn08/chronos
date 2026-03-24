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
