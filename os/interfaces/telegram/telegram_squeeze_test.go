package telegram

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestBot_Start_ContextAlreadyCanceled_Squeeze(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	b := New("token", func(context.Context, int64, int64, string) (string, error) {
		return "", nil
	})
	err := b.Start(ctx)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Start() = %v want context.Canceled", err)
	}
}

func TestWebhookHandler_BodyReadError_Squeeze(t *testing.T) {
	t.Parallel()
	b := New("t", echoHandler)
	req := httptest.NewRequest(http.MethodPost, "/hook", errReader{})
	w := httptest.NewRecorder()
	b.WebhookHandler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%q", w.Code, w.Body.String())
	}
}
