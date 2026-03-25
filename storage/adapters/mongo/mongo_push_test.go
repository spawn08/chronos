package mongo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetMemory_DocumentNotFound_Push(t *testing.T) {
	// Omit "document" so JSON unmarshals to nil RawMessage (JSON null is non-nil []byte).
	srv := newMongoTestServer(t, func(action string, body map[string]any) (int, any) {
		return http.StatusOK, map[string]any{}
	})
	defer srv.Close()

	s, _ := New(srv.URL, "", "db", "ds")
	_, err := s.GetMemory(context.Background(), "a1", "k1")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected document not found, got %v", err)
	}
}

func TestGetTrace_DocumentNotFound_Push(t *testing.T) {
	srv := newMongoTestServer(t, func(action string, body map[string]any) (int, any) {
		return http.StatusOK, map[string]any{}
	})
	defer srv.Close()

	s, _ := New(srv.URL, "", "db", "ds")
	_, err := s.GetTrace(context.Background(), "t1")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected document not found, got %v", err)
	}
}

func TestGetCheckpoint_DocumentNotFound_Push(t *testing.T) {
	srv := newMongoTestServer(t, func(action string, body map[string]any) (int, any) {
		return http.StatusOK, map[string]any{}
	})
	defer srv.Close()

	s, _ := New(srv.URL, "", "db", "ds")
	_, err := s.GetCheckpoint(context.Background(), "cp1")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected document not found, got %v", err)
	}
}

func TestFindOne_OuterJSONInvalid_Push(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"document":`)) // truncated
	}))
	defer srv.Close()

	s, _ := New(srv.URL, "", "db", "ds")
	_, err := s.GetMemory(context.Background(), "a1", "k")
	if err == nil || (!strings.Contains(err.Error(), "decode") && !strings.Contains(err.Error(), "mongo")) {
		t.Fatalf("expected decode error from HTTP body, got %v", err)
	}
}

func TestFindOne_DocumentDecodeError_Push(t *testing.T) {
	srv := newMongoTestServer(t, func(action string, body map[string]any) (int, any) {
		// Valid wrapper but document is not a JSON object for MemoryRecord
		return http.StatusOK, map[string]any{"document": json.RawMessage(`"scalar-not-object"`)}
	})
	defer srv.Close()

	s, _ := New(srv.URL, "", "db", "ds")
	_, err := s.GetMemory(context.Background(), "a1", "k")
	if err == nil || !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("expected document unmarshal error, got %v", err)
	}
}

func TestGetTrace_DocumentDecodeError_Push(t *testing.T) {
	srv := newMongoTestServer(t, func(action string, body map[string]any) (int, any) {
		return http.StatusOK, map[string]any{"document": json.RawMessage(`[]`)}
	})
	defer srv.Close()

	s, _ := New(srv.URL, "", "db", "ds")
	_, err := s.GetTrace(context.Background(), "t1")
	if err == nil || !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("expected document unmarshal error, got %v", err)
	}
}

func TestDo_ConnectionRefused_Push(t *testing.T) {
	// No server on this port — client.Do fails before JSON decode.
	s, _ := New("http://127.0.0.1:1", "", "db", "ds")
	_, err := s.GetSession(context.Background(), "any")
	if err == nil || (!strings.Contains(err.Error(), "findOne") && !strings.Contains(err.Error(), "mongo")) {
		t.Fatalf("expected connection error wrapping findOne/mongo, got %v", err)
	}
}

func TestGetCheckpoint_DocumentDecodeError_Push(t *testing.T) {
	srv := newMongoTestServer(t, func(action string, body map[string]any) (int, any) {
		return http.StatusOK, map[string]any{"document": json.RawMessage(`[]`)}
	})
	defer srv.Close()

	s, _ := New(srv.URL, "", "db", "ds")
	_, err := s.GetCheckpoint(context.Background(), "cp1")
	if err == nil || !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("expected document unmarshal error, got %v", err)
	}
}
