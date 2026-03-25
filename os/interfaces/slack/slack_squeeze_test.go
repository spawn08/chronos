package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type slackErrReader struct{}

func (slackErrReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestServeHTTP_BodyReadError_Squeeze(t *testing.T) {
	t.Parallel()
	b := buildBot()
	req := httptest.NewRequest(http.MethodPost, "/slack/events", slackErrReader{})
	w := httptest.NewRecorder()
	b.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestHandleMessage_EmptyResponse_Squeeze(t *testing.T) {
	t.Parallel()
	silent := New("tok", "sec", func(context.Context, string, string, string, string) (string, error) {
		return "", nil
	})

	payload := map[string]any{
		"type": "event_callback",
		"event": map[string]any{
			"type":    "message",
			"channel": "C1",
			"user":    "U1",
			"text":    "x",
		},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/slack/events", bytes.NewReader(body))
	w := httptest.NewRecorder()
	silent.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d", w.Code)
	}
	time.Sleep(20 * time.Millisecond)
}
