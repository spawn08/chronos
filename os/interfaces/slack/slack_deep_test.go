package slack

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

type slackRewriteRT struct {
	base *url.URL
	rt   http.RoundTripper
}

func (r *slackRewriteRT) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = r.base.Scheme
	req.URL.Host = r.base.Host
	return r.rt.RoundTrip(req)
}

func TestPostMessage_APIError_Deep(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":false,"error":"invalid_auth"}`)
	}))
	defer srv.Close()

	base, _ := url.Parse(srv.URL)
	b := New("x-token", "signing", func(context.Context, string, string, string, string) (string, error) {
		return "", nil
	})
	inner := srv.Client()
	b.httpClient = &http.Client{
		Timeout:   5 * time.Second,
		Transport: &slackRewriteRT{base: base, rt: inner.Transport},
	}

	err := b.PostMessage(context.Background(), "C1", "hello", "")
	if err == nil {
		t.Fatal("expected slack API error")
	}
}

func TestPostMessage_OK_Deep(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	base, _ := url.Parse(srv.URL)
	b := New("x-token", "signing", func(context.Context, string, string, string, string) (string, error) {
		return "", nil
	})
	inner := srv.Client()
	b.httpClient = &http.Client{
		Timeout:   5 * time.Second,
		Transport: &slackRewriteRT{base: base, rt: inner.Transport},
	}

	if err := b.PostMessage(context.Background(), "C1", "hello", ""); err != nil {
		t.Fatal(err)
	}
}
