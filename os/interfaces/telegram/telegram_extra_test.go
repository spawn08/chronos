package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// buildBotWithClient creates a bot using the given test server as the Telegram API.
func buildBotWithClient(handler MessageHandler, srv *httptest.Server) *Bot {
	b := New("test-token", handler)
	b.client = &http.Client{Timeout: 5 * time.Second}
	// Patch the token so requests go to the test server
	// We override via the client transport to redirect all requests to the test server.
	b.client.Transport = &redirectTransport{srv: srv}
	return b
}

// redirectTransport rewrites all requests to go to the given test server.
type redirectTransport struct {
	srv *httptest.Server
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Replace the host with the test server's URL
	newURL := t.srv.URL + req.URL.Path
	if req.URL.RawQuery != "" {
		newURL += "?" + req.URL.RawQuery
	}
	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	for key, vals := range req.Header {
		for _, v := range vals {
			newReq.Header.Add(key, v)
		}
	}
	return http.DefaultTransport.RoundTrip(newReq)
}

func TestSendMessage_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	b := buildBotWithClient(echoHandler, srv)
	err := b.SendMessage(context.Background(), 12345, "hello test")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
}

func TestSendMessage_NotOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"ok":false,"description":"bot blocked"}`)
	}))
	defer srv.Close()

	b := buildBotWithClient(echoHandler, srv)
	err := b.SendMessage(context.Background(), 12345, "hello")
	if err == nil {
		t.Fatal("expected error when ok=false")
	}
	if !strings.Contains(err.Error(), "bot blocked") {
		t.Errorf("error=%v", err)
	}
}

func TestSendMessage_NetworkError(t *testing.T) {
	b := New("tok", echoHandler)
	b.client = &http.Client{Timeout: 100 * time.Millisecond}
	// Point to a non-existent server
	// This will fail at transport level but the actual URL is hardcoded to api.telegram.org
	// so we just verify the error is wrapped properly by using a closed server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.Close() // immediately close

	b.client.Transport = &redirectTransport{srv: srv}
	err := b.SendMessage(context.Background(), 1, "hi")
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestSendInlineKeyboard_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	b := buildBotWithClient(echoHandler, srv)
	buttons := [][]Button{
		{
			{Text: "Yes", CallbackData: "yes"},
			{Text: "No", CallbackData: "no"},
		},
	}
	err := b.SendInlineKeyboard(context.Background(), 12345, "Approve?", buttons)
	if err != nil {
		t.Fatalf("SendInlineKeyboard: %v", err)
	}
}

func TestSendInlineKeyboard_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	b := buildBotWithClient(echoHandler, srv)
	buttons := [][]Button{{}}
	err := b.SendInlineKeyboard(context.Background(), 1, "test", buttons)
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestPollOnce_WithMessages(t *testing.T) {
	updateBody, _ := json.Marshal(map[string]any{
		"ok": true,
		"result": []map[string]any{
			{
				"update_id": 100,
				"message": map[string]any{
					"chat": map[string]any{"id": 42},
					"from": map[string]any{"id": 99},
					"text": "poll test",
				},
			},
		},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(updateBody)
	}))
	defer srv.Close()

	b := buildBotWithClient(echoHandler, srv)
	err := b.pollOnce(context.Background())
	if err != nil {
		t.Fatalf("pollOnce: %v", err)
	}
	if b.offset != 101 {
		t.Errorf("offset=%d, want 101", b.offset)
	}
}

func TestPollOnce_EmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"ok":true,"result":[]}`)
	}))
	defer srv.Close()

	b := buildBotWithClient(echoHandler, srv)
	err := b.pollOnce(context.Background())
	if err != nil {
		t.Fatalf("pollOnce: %v", err)
	}
}

func TestPollOnce_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "not-json")
	}))
	defer srv.Close()

	b := buildBotWithClient(echoHandler, srv)
	err := b.pollOnce(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestPollOnce_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	b := buildBotWithClient(echoHandler, srv)
	err := b.pollOnce(context.Background())
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestHandleUpdate_HandlerError(t *testing.T) {
	errHandler := func(_ context.Context, chatID int64, _ int64, _ string) (string, error) {
		return "", fmt.Errorf("handler error")
	}

	sendCallCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sendCallCount++
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	b := buildBotWithClient(errHandler, srv)
	b.handleUpdate(context.Background(), 1, 2, "test")
	// Give goroutine time if any
	time.Sleep(10 * time.Millisecond)
}

func TestHandleUpdate_EmptyResponse(t *testing.T) {
	emptyHandler := func(_ context.Context, _ int64, _ int64, _ string) (string, error) {
		return "", nil // empty response — should not call SendMessage
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	b := buildBotWithClient(emptyHandler, srv)
	b.handleUpdate(context.Background(), 1, 2, "test")
}

func TestStart_StopSignal(t *testing.T) {
	// Return empty updates so Start loops properly
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"ok":true,"result":[]}`)
	}))
	defer srv.Close()

	b := buildBotWithClient(echoHandler, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Stop the bot quickly from another goroutine
	go func() {
		time.Sleep(50 * time.Millisecond)
		b.Stop()
	}()

	err := b.Start(ctx)
	// Should return nil (stopped via stopCh) or context error
	if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		t.Errorf("Start returned unexpected error: %v", err)
	}
}
