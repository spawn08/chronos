package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// roundTripFunc implements http.RoundTripper for tests.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestSendMessage_SuccessWithMockTransport(t *testing.T) {
	var gotMethod, gotPath string
	b := New("tok", echoHandler)
	b.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotMethod = req.Method
			gotPath = req.URL.Path
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	ctx := context.Background()
	err := b.SendMessage(ctx, "chan-1", "hello discord")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method=%q", gotMethod)
	}
	if !strings.Contains(gotPath, "chan-1") {
		t.Errorf("path=%q", gotPath)
	}
}

func TestSendMessage_HTTPErrorWithMockTransport(t *testing.T) {
	b := New("tok", echoHandler)
	b.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(strings.NewReader(`{"message":"bad"}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	err := b.SendMessage(context.Background(), "c", "x")
	if err == nil {
		t.Fatal("expected error for 4xx")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("err=%v", err)
	}
}

func TestSendMessage_NetworkErrorWithMockTransport(t *testing.T) {
	b := New("tok", echoHandler)
	b.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("no route")
		}),
	}

	err := b.SendMessage(context.Background(), "c", "x")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHandleInteraction_BodyReadError(t *testing.T) {
	b := buildBot()
	req := httptest.NewRequest(http.MethodPost, "/interactions", io.NopCloser(errReader{}))
	w := httptest.NewRecorder()
	b.HandleInteraction(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("code=%d", w.Code)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestHandleInteraction_HandlerErrorStillAcks(t *testing.T) {
	failing := func(ctx context.Context, channelID, userID, content string) (string, error) {
		return "", errors.New("agent failed")
	}
	b := New("tok", failing)

	payload := map[string]any{
		"type": 2,
		"data": map[string]any{
			"name": "chat",
			"options": []map[string]any{
				{"name": "message", "value": "x"},
			},
		},
		"channel_id": "C1",
		"member": map[string]any{
			"user": map[string]any{"id": "U1"},
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/i", bytes.NewReader(body))
	w := httptest.NewRecorder()
	b.HandleInteraction(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("code=%d", w.Code)
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["type"] != float64(5) {
		t.Errorf("expected deferred ack, got %v", resp)
	}
}
