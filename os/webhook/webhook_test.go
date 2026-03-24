package webhook

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServer_BasicEvent(t *testing.T) {
	s := NewServer("")
	var received Event
	s.On("test", func(_ context.Context, e Event) error {
		received = e
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{"key":"value"}`))
	req.Header.Set("X-Event-Type", "test")
	req.Header.Set("X-Event-Source", "unit-test")
	w := httptest.NewRecorder()

	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
	if received.Type != "test" {
		t.Errorf("type = %q", received.Type)
	}
	if received.Source != "unit-test" {
		t.Errorf("source = %q", received.Source)
	}
}

func TestServer_SecretValidation(t *testing.T) {
	s := NewServer("my-secret")

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{}`))
	req.Header.Set("X-Event-Type", "test")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without secret, got %d", w.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{}`))
	req2.Header.Set("X-Event-Type", "test")
	req2.Header.Set("X-Webhook-Secret", "my-secret")
	w2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 with correct secret, got %d", w2.Code)
	}
}

func TestServer_WildcardHandler(t *testing.T) {
	s := NewServer("")
	called := false
	s.On("*", func(_ context.Context, e Event) error {
		called = true
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{}`))
	req.Header.Set("X-Event-Type", "anything")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if !called {
		t.Error("wildcard handler not called")
	}
}

func TestServer_MethodNotAllowed(t *testing.T) {
	s := NewServer("")
	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestServer_HandlerError(t *testing.T) {
	s := NewServer("")
	s.On("test", func(_ context.Context, e Event) error {
		return fmt.Errorf("handler failed")
	})

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{}`))
	req.Header.Set("X-Event-Type", "test")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on handler error, got %d", w.Code)
	}
}

func TestServer_DefaultEventType(t *testing.T) {
	s := NewServer("")
	var received Event
	s.On("generic", func(_ context.Context, e Event) error {
		received = e
		return nil
	})

	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)

	if received.Type != "generic" {
		t.Errorf("type = %q, want generic", received.Type)
	}
}
