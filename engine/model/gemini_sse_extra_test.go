package model

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGemini_readSSEStream_EmitsDelta(t *testing.T) {
	payload := `data: {"candidates":[{"content":{"parts":[{"text":"Hello"}]},"finishReason":""}]}` + "\n\n"
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(payload))}
	ch := make(chan *ChatResponse, 4)
	g := NewGemini("key")
	go func() {
		g.readSSEStream(resp, ch)
		close(ch)
	}()
	var got string
	for c := range ch {
		got += c.Content
	}
	if got != "Hello" {
		t.Errorf("got %q", got)
	}
}

func TestGemini_readSSEStream_SkipsBadJSON(t *testing.T) {
	payload := `data: not-json` + "\n\n" +
		`data: {"candidates":[{"content":{"parts":[{"text":"x"}]},"finishReason":""}]}` + "\n\n"
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(payload))}
	ch := make(chan *ChatResponse, 4)
	g := NewGemini("key")
	go func() {
		g.readSSEStream(resp, ch)
		close(ch)
	}()
	n := 0
	for range ch {
		n++
	}
	if n != 1 {
		t.Errorf("expected 1 chunk, got %d", n)
	}
}
