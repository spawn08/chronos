package dynamo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
