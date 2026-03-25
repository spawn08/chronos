package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func echoHandler(_ context.Context, chatID, userID int64, text string) (string, error) {
	return "echo:" + text, nil
}

func buildBot() *Bot {
	return New("test-bot-token", echoHandler)
}

func TestNew(t *testing.T) {
	b := buildBot()
	if b == nil {
		t.Fatal("New returned nil")
	}
	if b.token != "test-bot-token" {
		t.Errorf("token: got %q", b.token)
	}
	if b.handler == nil {
		t.Error("handler should not be nil")
	}
	if b.client == nil {
		t.Error("client should not be nil")
	}
	if b.stopCh == nil {
		t.Error("stopCh should not be nil")
	}
}

func TestStop(t *testing.T) {
	b := buildBot()
	// Should not panic
	b.Stop()
	// Idempotent
	b.Stop()
}

func TestStopCancelsContext(t *testing.T) {
	b := buildBot()
	b.Stop()
	select {
	case <-b.stopCh:
		// closed — good
	default:
		t.Error("stopCh should be closed after Stop()")
	}
}

func TestWebhookHandlerValidMessage(t *testing.T) {
	b := buildBot()

	update := map[string]any{
		"message": map[string]any{
			"chat": map[string]any{"id": float64(12345)},
			"from": map[string]any{"id": float64(67890)},
			"text": "hello bot",
		},
	}
	body, _ := json.Marshal(update)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	w := httptest.NewRecorder()
	b.WebhookHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestWebhookHandlerNoMessage(t *testing.T) {
	b := buildBot()

	update := map[string]any{}
	body, _ := json.Marshal(update)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	w := httptest.NewRecorder()
	b.WebhookHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestWebhookHandlerEmptyText(t *testing.T) {
	b := buildBot()

	update := map[string]any{
		"message": map[string]any{
			"chat": map[string]any{"id": float64(1)},
			"from": map[string]any{"id": float64(2)},
			"text": "", // empty — should not dispatch
		},
	}
	body, _ := json.Marshal(update)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	w := httptest.NewRecorder()
	b.WebhookHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestWebhookHandlerInvalidJSON(t *testing.T) {
	b := buildBot()

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString("{bad"))
	w := httptest.NewRecorder()
	b.WebhookHandler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestWebhookHandlerEmptyBody(t *testing.T) {
	b := buildBot()

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString(""))
	w := httptest.NewRecorder()
	b.WebhookHandler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty body, got %d", w.Code)
	}
}

func TestButtonStruct(t *testing.T) {
	btn := Button{
		Text:         "Approve",
		CallbackData: "approve:task-1",
	}
	if btn.Text != "Approve" {
		t.Errorf("Text: got %q", btn.Text)
	}
	if btn.CallbackData != "approve:task-1" {
		t.Errorf("CallbackData: got %q", btn.CallbackData)
	}
}

func TestStartCancelledContext(t *testing.T) {
	b := buildBot()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := b.Start(ctx)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}
