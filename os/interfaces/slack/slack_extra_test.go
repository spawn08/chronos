package slack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPostMessage_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	b := buildBot()
	err := b.PostMessage(ctx, "C123", "hello", "")
	// Should fail with a context error since we can't reach slack.com
	if err == nil {
		t.Log("PostMessage succeeded (network available)")
	}
}

func TestPostMessage_WithThreadTS(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	b := buildBot()
	err := b.PostMessage(ctx, "C123", "hello", "12345.67890")
	if err == nil {
		t.Log("PostMessage succeeded")
	}
}

func TestStart_CancelContext(t *testing.T) {
	b := buildBot()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- b.Start(ctx, "127.0.0.1:0")
	}()

	select {
	case err := <-errCh:
		_ = err
	case <-time.After(500 * time.Millisecond):
		b.Stop()
	}
}

func TestHandleMessage_BotMessage(t *testing.T) {
	b := buildBot()
	payload := `{"type":"event_callback","event":{"type":"message","channel":"C1","user":"U1","text":"hi","bot_id":"BOT123"}}`
	req := httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(payload))
	w := httptest.NewRecorder()
	b.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleMessage_EmptyText(t *testing.T) {
	b := buildBot()
	payload := `{"type":"event_callback","event":{"type":"message","channel":"C1","user":"U1","text":""}}`
	req := httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(payload))
	w := httptest.NewRecorder()
	b.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleMessage_WithThread(t *testing.T) {
	b := buildBot()
	payload := `{"type":"event_callback","event":{"type":"message","channel":"C1","user":"U1","text":"hello","thread_ts":"12345.0"}}`
	req := httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(payload))
	w := httptest.NewRecorder()
	b.ServeHTTP(w, req)
	// Give goroutine time to fire
	time.Sleep(10 * time.Millisecond)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestStop_WithNoServer(t *testing.T) {
	b := buildBot()
	err := b.Stop()
	if err != nil {
		t.Errorf("Stop with no server: %v", err)
	}
}

func TestStart_ListenAndServeFails(t *testing.T) {
	b := buildBot()
	ctx := context.Background()

	// Try to start on an invalid address
	errCh := make(chan error, 1)
	go func() {
		errCh <- b.Start(ctx, "invalid-address:99999")
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected error for invalid address")
		}
	case <-time.After(2 * time.Second):
		t.Error("timed out waiting for Start to fail")
	}
}

func TestBotServer_ServesOnEvents(t *testing.T) {
	b := buildBot()

	// Test that the ServeHTTP handler works at the /slack/events path
	// via a fake HTTP server using the Bot as handler
	srv := httptest.NewServer(b)
	defer srv.Close()

	payload := `{"type":"url_verification","challenge":"test-token-abc"}`
	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status=%d, want 200", resp.StatusCode)
	}
}
