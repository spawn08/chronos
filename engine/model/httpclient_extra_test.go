package model

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewHTTPClient_DefaultTimeout(t *testing.T) {
	h := newHTTPClient("http://example.com", 0, map[string]string{"X-Test": "1"})
	if h.client.Timeout == 0 {
		t.Fatal("expected positive default timeout")
	}
}

func TestHTTPClient_post_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "yes" {
			t.Error("missing custom header")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	h := newHTTPClient(srv.URL, 5, map[string]string{"X-Custom": "yes"})
	resp, err := h.post(context.Background(), "/v1/x", map[string]string{"a": "b"})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestHTTPClient_post_RequestError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.Close()
	h := newHTTPClient(srv.URL, 1, nil)
	_, err := h.post(context.Background(), "/", map[string]string{"a": "b"})
	if err == nil {
		t.Fatal("expected error when server is not accepting connections")
	}
}

func TestReadErrorBody_ReadFails(t *testing.T) {
	resp := &http.Response{
		Status:     "500 Internal Server Error",
		StatusCode: 500,
		Body:       io.NopCloser(errReader{}),
	}
	got := readErrorBody(resp)
	if got != "500 Internal Server Error" {
		t.Errorf("got %q, want status only when body read fails", got)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}
