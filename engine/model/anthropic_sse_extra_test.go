package model

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAnthropic_readSSEStream_TextDeltaAndStop(t *testing.T) {
	payload := `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}` + "\n\n" +
		`data: {"type":"message_stop"}` + "\n\n"
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(payload))}
	ch := make(chan *ChatResponse, 8)
	a := NewAnthropic("sk-test")
	go func() {
		a.readSSEStream(resp, ch)
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
