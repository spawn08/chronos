package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func echoHandler(_ context.Context, channelID, userID, content string) (string, error) {
	return "echo:" + content, nil
}

func buildBot() *Bot {
	return New("test-discord-token", echoHandler)
}

func TestNew(t *testing.T) {
	b := buildBot()
	if b == nil {
		t.Fatal("New returned nil")
	}
	if b.token != "test-discord-token" {
		t.Errorf("token: got %q", b.token)
	}
	if b.handler == nil {
		t.Error("handler should not be nil")
	}
}

func TestPingInteraction(t *testing.T) {
	b := buildBot()

	payload := map[string]any{
		"type": 1, // PING
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/discord/interactions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	b.HandleInteraction(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["type"] != float64(1) {
		t.Errorf("expected pong type=1, got %v", resp["type"])
	}
}

func TestApplicationCommandInteraction(t *testing.T) {
	b := buildBot()

	payload := map[string]any{
		"type": 2,
		"data": map[string]any{
			"name": "chat",
			"options": []map[string]any{
				{"name": "message", "value": "hello"},
			},
		},
		"channel_id": "C001",
		"member": map[string]any{
			"user": map[string]any{"id": "U001"},
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/discord/interactions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	b.HandleInteraction(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// Should respond with deferred type=5
	if resp["type"] != float64(5) {
		t.Errorf("expected deferred type=5, got %v", resp["type"])
	}
}

func TestUnknownInteractionType(t *testing.T) {
	b := buildBot()

	payload := map[string]any{
		"type": 99,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/discord/interactions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	b.HandleInteraction(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestHandleInteractionInvalidJSON(t *testing.T) {
	b := buildBot()

	req := httptest.NewRequest(http.MethodPost, "/discord/interactions", bytes.NewBufferString("{invalid"))
	w := httptest.NewRecorder()
	b.HandleInteraction(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestStop(t *testing.T) {
	b := buildBot()
	// Should not panic
	b.Stop()
	// Second call should be idempotent
	b.Stop()
}

func TestHandleInteractionEmptyBody(t *testing.T) {
	b := buildBot()

	req := httptest.NewRequest(http.MethodPost, "/discord/interactions", bytes.NewBufferString(""))
	w := httptest.NewRecorder()
	b.HandleInteraction(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty body, got %d", w.Code)
	}
}

func TestNewBotStopCh(t *testing.T) {
	b := buildBot()
	if b.stopCh == nil {
		t.Error("stopCh should not be nil")
	}
}
