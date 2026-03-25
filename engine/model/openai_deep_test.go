package model

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestReadOpenAISSEStream_Done_Deep(t *testing.T) {
	body := strings.NewReader("foo\n\ndata: [DONE]\n")
	resp := &http.Response{Body: io.NopCloser(body)}
	ch := make(chan *ChatResponse, 8)
	readOpenAISSEStream(resp, ch)
}

func TestReadOpenAISSEStream_InvalidJSONSkipped_Deep(t *testing.T) {
	body := strings.NewReader("data: {not-json\n\ndata: [DONE]\n")
	resp := &http.Response{Body: io.NopCloser(body)}
	ch := make(chan *ChatResponse, 8)
	readOpenAISSEStream(resp, ch)
}

func TestReadOpenAISSEStream_EmptyChoices_Deep(t *testing.T) {
	body := strings.NewReader(`data: {"id":"x","choices":[]}` + "\n\n" + "data: [DONE]\n")
	resp := &http.Response{Body: io.NopCloser(body)}
	ch := make(chan *ChatResponse, 8)
	readOpenAISSEStream(resp, ch)
	select {
	case <-ch:
		t.Fatal("unexpected chunk for empty choices")
	default:
	}
}

func TestReadOpenAISSEStream_ContentDelta_Deep(t *testing.T) {
	chunk := `{"id":"c1","choices":[{"delta":{"content":"hi"}}]}`
	body := strings.NewReader("data: " + chunk + "\n\n" + "data: [DONE]\n")
	resp := &http.Response{Body: io.NopCloser(body)}
	ch := make(chan *ChatResponse, 8)
	readOpenAISSEStream(resp, ch)
	got := <-ch
	if got.Content != "hi" || !got.Delta {
		t.Fatalf("got %+v", got)
	}
}

func TestReadOpenAISSEStream_ToolCallsDelta_Deep(t *testing.T) {
	chunk := `{"id":"t1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call1","type":"function","function":{"name":"alpha","arguments":"{}"}}]}}]}`
	body := strings.NewReader("data: " + chunk + "\n\n" + "data: [DONE]\n")
	resp := &http.Response{Body: io.NopCloser(body)}
	ch := make(chan *ChatResponse, 8)
	readOpenAISSEStream(resp, ch)
	got := <-ch
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].Name != "alpha" {
		t.Fatalf("got %+v", got.ToolCalls)
	}
}

func TestReadOpenAISSEStream_MultipleTextChunks_Deep(t *testing.T) {
	body := strings.NewReader("data: {\"id\":\"m\",\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n" +
		"data: {\"id\":\"m\",\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n" +
		"data: [DONE]\n")
	resp := &http.Response{Body: io.NopCloser(body)}
	ch := make(chan *ChatResponse, 8)
	readOpenAISSEStream(resp, ch)
	<-ch
	<-ch
}
