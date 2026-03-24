package mongo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spawn08/chronos/storage"
)

func TestFindOne_DocumentNotFound_ITER6(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Empty object: "document" absent → wrapper.Document nil → not found
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s, _ := New(srv.URL, "", "db", "ds")
	_, err := s.GetSession(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected document not found error")
	}
}

func TestDo_InvalidJSONBody_ITER6(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	s, _ := New(srv.URL, "", "db", "ds")
	_, err := s.do(context.Background(), "findOne", "sessions", map[string]any{"filter": map[string]any{}})
	if err == nil {
		t.Fatal("expected decode error from do")
	}
}

func TestFind_WrapperUnmarshalError_ITER6(t *testing.T) {
	srv := newMongoTestServer(t, func(action string, body map[string]any) (int, any) {
		if action == "find" {
			// Valid JSON from do(), but not an object — find() cannot unmarshal into wrapper struct
			return http.StatusOK, 123
		}
		return http.StatusOK, map[string]any{"documents": []any{}}
	})
	defer srv.Close()

	s, _ := New(srv.URL, "", "db", "ds")
	_, err := s.find(context.Background(), "sessions", map[string]any{"agent_id": "a1"}, map[string]any{"created_at": -1}, 10)
	if err == nil {
		t.Fatal("expected find wrapper unmarshal error")
	}
}

func TestFindOne_WrapperUnmarshalError_ITER6(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"document": 123}`))
	}))
	defer srv.Close()

	s, _ := New(srv.URL, "", "db", "ds")
	var sess storage.Session
	err := s.findOne(context.Background(), "sessions", map[string]any{"id": "x"}, &sess)
	if err == nil {
		t.Fatal("expected unmarshal into session to fail")
	}
}

func TestDo_ClientRoundTripError_ITER6(t *testing.T) {
	s, _ := New("http://127.0.0.1:65433", "", "db", "ds")
	_, err := s.do(context.Background(), "insertOne", "sessions", map[string]any{"document": map[string]any{}})
	if err == nil {
		t.Fatal("expected network error")
	}
}
