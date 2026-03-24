package slack

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestPostMessage_OKWithMockTransport(t *testing.T) {
	b := buildBot()
	b.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	err := b.PostMessage(context.Background(), "C1", "text", "")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
}

func TestPostMessage_SlackAPIErrorWithMockTransport(t *testing.T) {
	b := buildBot()
	b.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"ok":false,"error":"channel_not_found"}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	err := b.PostMessage(context.Background(), "C1", "text", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "channel_not_found") {
		t.Errorf("err=%v", err)
	}
}

func TestPostMessage_RoundTripError(t *testing.T) {
	b := buildBot()
	b.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, errors.New("unreachable")
		}),
	}

	err := b.PostMessage(context.Background(), "C1", "text", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHandleMessage_HandlerError(t *testing.T) {
	errHandler := func(ctx context.Context, channel, user, text, threadTS string) (string, error) {
		return "", errors.New("agent failed")
	}
	b := New("xoxb-test-token", "signing-secret", errHandler)
	b.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	b.handleMessage(context.Background(), "C1", "U1", "hi", "")
}
