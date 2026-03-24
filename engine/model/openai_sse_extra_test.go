package model

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestReadOpenAISSEStream_ContentDelta(t *testing.T) {
	body := strings.NewReader(
		`data: {"id":"chunk-1","choices":[{"delta":{"content":"Hello"}}]}` + "\n\n" +
			`data: {"id":"chunk-1","choices":[{"delta":{"content":" world"}}]}` + "\n\n" +
			`data: [DONE]` + "\n",
	)
	resp := &http.Response{Body: io.NopCloser(body)}
	ch := make(chan *ChatResponse, 16)
	go func() {
		readOpenAISSEStream(resp, ch)
		close(ch)
	}()
	var parts []string
	for c := range ch {
		parts = append(parts, c.Content)
	}
	got := strings.Join(parts, "")
	if got != "Hello world" {
		t.Errorf("got %q", got)
	}
}

func TestReadOpenAISSEStream_SkipsNonDataLinesAndBadJSON(t *testing.T) {
	body := strings.NewReader(
		": ping\n\n" +
			`data: not-json` + "\n\n" +
			`data: {"id":"x","choices":[]}` + "\n\n" +
			`data: {"id":"y","choices":[{"delta":{"content":"ok"}}]}` + "\n\n",
	)
	resp := &http.Response{Body: io.NopCloser(body)}
	ch := make(chan *ChatResponse, 8)
	go func() {
		readOpenAISSEStream(resp, ch)
		close(ch)
	}()
	var last *ChatResponse
	for c := range ch {
		last = c
	}
	if last == nil || last.Content != "ok" {
		t.Fatalf("unexpected stream: %+v", last)
	}
}
