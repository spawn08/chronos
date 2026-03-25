package webhook

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type errBody struct{}

func (errBody) Read([]byte) (int, error) { return 0, errors.New("body read error") }
func (errBody) Close() error             { return nil }

func TestServer_BodyReadError_Boost(t *testing.T) {
	s := NewServer("")
	s.On("test", func(context.Context, Event) error { return nil })

	req := httptest.NewRequest(http.MethodPost, "/webhook", http.NoBody)
	req.Body = io.NopCloser(errBody{})
	req.Header.Set("X-Event-Type", "test")
	w := httptest.NewRecorder()

	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestServer_SubpathWebhook_Boost(t *testing.T) {
	s := NewServer("")
	var got string
	s.On("evt", func(_ context.Context, e Event) error {
		got = string(e.Body)
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/webhook/custom/path", strings.NewReader(`{"x":1}`))
	req.Header.Set("X-Event-Type", "evt")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if !strings.Contains(got, "x") {
		t.Errorf("body = %q", got)
	}
}
