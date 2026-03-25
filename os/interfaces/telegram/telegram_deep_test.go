package telegram

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

type rewriteRoundTripper struct {
	base *url.URL
	rt   http.RoundTripper
}

func (r *rewriteRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = r.base.Scheme
	req.URL.Host = r.base.Host
	return r.rt.RoundTrip(req)
}

func TestPollOnce_OK_EmptyResult_Deep(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"result":[]}`)
	}))
	defer srv.Close()

	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	b := New("tok", echoHandler)
	inner := srv.Client()
	b.client = &http.Client{
		Timeout:   5 * time.Second,
		Transport: &rewriteRoundTripper{base: base, rt: inner.Transport},
	}

	if err := b.pollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSendMessage_APIError_Deep(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":false,"description":"bad token"}`)
	}))
	defer srv.Close()

	base, _ := url.Parse(srv.URL)
	b := New("tok", echoHandler)
	inner := srv.Client()
	b.client = &http.Client{
		Timeout:   5 * time.Second,
		Transport: &rewriteRoundTripper{base: base, rt: inner.Transport},
	}

	err := b.SendMessage(context.Background(), 1, "hi")
	if err == nil {
		t.Fatal("expected telegram API error")
	}
}

func TestWebhookHandler_InvalidJSON_Deep(t *testing.T) {
	b := buildBot()
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewBufferString("{"))
	w := httptest.NewRecorder()
	b.WebhookHandler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", w.Code)
	}
}
