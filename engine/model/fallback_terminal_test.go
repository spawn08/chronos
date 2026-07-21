package model

import (
	"context"
	"fmt"
	"testing"
)

// errProvider fails every call with a fixed error.
type errProvider struct {
	name string
	err  error
}

func (e *errProvider) Name() string  { return e.name }
func (e *errProvider) Model() string { return e.name }
func (e *errProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	return nil, e.err
}
func (e *errProvider) StreamChat(_ context.Context, _ *ChatRequest) (<-chan *ChatResponse, error) {
	return nil, e.err
}

// countProvider records how many times it was invoked, then succeeds.
type countProvider struct {
	calls int
}

func (c *countProvider) Name() string  { return "count" }
func (c *countProvider) Model() string { return "count" }
func (c *countProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	c.calls++
	return &ChatResponse{Content: "ok"}, nil
}
func (c *countProvider) StreamChat(_ context.Context, _ *ChatRequest) (<-chan *ChatResponse, error) {
	c.calls++
	ch := make(chan *ChatResponse)
	close(ch)
	return ch, nil
}

func TestFallback_StopsOnTerminalError(t *testing.T) {
	terminal := fmt.Errorf("openai chat: %w", &APIError{StatusCode: 400, Status: "400 Bad Request"})
	second := &countProvider{}
	fp, _ := NewFallbackProvider(&errProvider{name: "primary", err: terminal}, second)

	_, err := fp.Chat(context.Background(), &ChatRequest{})
	if err == nil {
		t.Fatal("expected error")
		return
	}
	if second.calls != 0 {
		t.Errorf("second provider was tried %d times; terminal errors must not fall through", second.calls)
	}
}

func TestFallback_FallsThroughOnRetryableError(t *testing.T) {
	retryable := fmt.Errorf("openai chat: %w", &APIError{StatusCode: 503, Status: "503 Service Unavailable"})
	second := &countProvider{}
	fp, _ := NewFallbackProvider(&errProvider{name: "primary", err: retryable}, second)

	resp, err := fp.Chat(context.Background(), &ChatRequest{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "ok" || second.calls != 1 {
		t.Errorf("expected fallthrough to second provider (calls=%d, content=%q)", second.calls, resp.Content)
	}
}
