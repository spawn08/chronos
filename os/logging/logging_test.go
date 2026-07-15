package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func decodeLine(t *testing.T, b []byte) map[string]any {
	t.Helper()
	line := strings.TrimSpace(string(b))
	if line == "" {
		return nil
	}
	// Only decode the first line.
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("log line is not valid JSON: %v\n%s", err, line)
	}
	return m
}

func TestLogger_EmitsValidJSONWithCorrelationID(t *testing.T) {
	var buf bytes.Buffer
	lg := New(&buf, withClock(func() time.Time { return time.Unix(0, 0).UTC() }))

	ctx := WithCorrelationID(context.Background(), "corr-123")
	ctx = WithTenant(ctx, "acme")
	lg.Info(ctx, "hello", "user", "alice", "count", 3)

	m := decodeLine(t, buf.Bytes())
	tests := map[string]any{
		"level":          "info",
		"msg":            "hello",
		"correlation_id": "corr-123",
		"tenant":         "acme",
		"user":           "alice",
		"count":          float64(3),
	}
	for k, want := range tests {
		if m[k] != want {
			t.Errorf("field %q = %v, want %v", k, m[k], want)
		}
	}
	if _, ok := m["ts"]; !ok {
		t.Error("missing ts field")
	}
}

func TestLogger_LevelFiltering(t *testing.T) {
	tests := []struct {
		name    string
		min     Level
		logAt   Level
		wantOut bool
	}{
		{"debug filtered at info", LevelInfo, LevelDebug, false},
		{"info emitted at info", LevelInfo, LevelInfo, true},
		{"error emitted at info", LevelInfo, LevelError, true},
		{"debug emitted at debug", LevelDebug, LevelDebug, true},
		{"warn filtered at error", LevelError, LevelWarn, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			lg := New(&buf, WithMinLevel(tt.min))
			lg.Log(context.Background(), tt.logAt, "m")
			got := buf.Len() > 0
			if got != tt.wantOut {
				t.Errorf("emitted = %v, want %v", got, tt.wantOut)
			}
		})
	}
}

func TestLogger_OddFields(t *testing.T) {
	var buf bytes.Buffer
	lg := New(&buf)
	lg.Info(context.Background(), "m", "onlykey")
	m := decodeLine(t, buf.Bytes())
	if m["_MISSING_VALUE"] != "onlykey" {
		t.Errorf("_MISSING_VALUE = %v, want onlykey", m["_MISSING_VALUE"])
	}
}

func TestLogger_StaticFields(t *testing.T) {
	var buf bytes.Buffer
	lg := New(&buf, WithField("service", "chronos-os"))
	lg.Info(context.Background(), "m")
	m := decodeLine(t, buf.Bytes())
	if m["service"] != "chronos-os" {
		t.Errorf("service = %v, want chronos-os", m["service"])
	}
}

func TestLogger_NoCorrelationIDWhenAbsent(t *testing.T) {
	var buf bytes.Buffer
	lg := New(&buf)
	lg.Info(context.Background(), "m")
	m := decodeLine(t, buf.Bytes())
	if _, ok := m["correlation_id"]; ok {
		t.Error("correlation_id should be absent when not set")
	}
}

func TestNewCorrelationID_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := NewCorrelationID()
		if id == "" {
			t.Fatal("empty correlation id")
		}
		if seen[id] {
			t.Fatalf("duplicate id: %s", id)
		}
		seen[id] = true
	}
}

func TestMiddleware_GeneratesCorrelationID(t *testing.T) {
	var buf bytes.Buffer
	lg := New(&buf)

	var seenID, seenTenant string
	handler := Middleware(lg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenID = CorrelationIDFromContext(r.Context())
		seenTenant = TenantFromContext(r.Context())
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
	req.Header.Set(HeaderTenant, "acme")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if seenID == "" {
		t.Error("handler context missing generated correlation id")
	}
	if rec.Header().Get(HeaderCorrelationID) != seenID {
		t.Errorf("response header id = %q, want %q", rec.Header().Get(HeaderCorrelationID), seenID)
	}
	if seenTenant != "acme" {
		t.Errorf("tenant = %q, want acme", seenTenant)
	}

	m := decodeLine(t, buf.Bytes())
	if m["status"] != float64(http.StatusTeapot) {
		t.Errorf("logged status = %v, want %d", m["status"], http.StatusTeapot)
	}
	if m["correlation_id"] != seenID {
		t.Errorf("logged correlation_id = %v, want %q", m["correlation_id"], seenID)
	}
}

func TestMiddleware_ReusesIncomingCorrelationID(t *testing.T) {
	handler := Middleware(New(&bytes.Buffer{}))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := CorrelationIDFromContext(r.Context()); got != "incoming-42" {
			t.Errorf("correlation id = %q, want incoming-42", got)
		}
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
	req.Header.Set(HeaderCorrelationID, "incoming-42")
	handler.ServeHTTP(httptest.NewRecorder(), req)
}

func TestContextHelpers_NilSafe(t *testing.T) {
	if CorrelationIDFromContext(context.Background()) != "" {
		t.Error("expected empty correlation id")
	}
	if TenantFromContext(context.Background()) != "" {
		t.Error("expected empty tenant")
	}
}
