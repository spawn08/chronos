package model

import (
	"context"
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
		a.readSSEStream(context.Background(), resp, ch)
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

func TestAnthropic_readSSEStream_ThinkingSignature(t *testing.T) {
	payload := strings.Join([]string{
		`data: {"type":"content_block_start","content_block":{"type":"thinking"}}`,
		`data: {"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"plan "}}`,
		`data: {"type":"content_block_delta","delta":{"type":"signature_delta","signature":"sig_1"}}`,
		`data: {"type":"content_block_stop"}`,
		`data: {"type":"message_stop"}`,
	}, "\n\n") + "\n\n"
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(payload))}
	ch := make(chan *ChatResponse, 8)
	a := NewAnthropic("sk-test")
	go func() {
		a.readSSEStream(context.Background(), resp, ch)
		close(ch)
	}()
	var reasoning string
	var state any
	for c := range ch {
		reasoning += c.Reasoning
		if c.ProviderState != nil {
			state = c.ProviderState
		}
	}
	if reasoning != "plan " {
		t.Errorf("reasoning = %q, want %q", reasoning, "plan ")
	}
	blocks, ok := state.([]map[string]any)
	if !ok || len(blocks) != 1 || blocks[0]["signature"] != "sig_1" || blocks[0]["thinking"] != "plan " {
		t.Fatalf("ProviderState = %#v", state)
	}
}

func TestAnthropic_readSSEStream_ThinkingFieldOnStart(t *testing.T) {
	payload := strings.Join([]string{
		`data: {"type":"content_block_start","content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`data: {"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"need a lookup"}}`,
		`data: {"type":"content_block_delta","delta":{"type":"signature_delta","signature":"sig_2"}}`,
		`data: {"type":"content_block_stop"}`,
		`data: {"type":"message_stop"}`,
	}, "\n\n") + "\n\n"
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(payload))}
	ch := make(chan *ChatResponse, 8)
	a := NewAnthropic("sk-test")
	go func() {
		a.readSSEStream(context.Background(), resp, ch)
		close(ch)
	}()
	var state any
	for c := range ch {
		if c.ProviderState != nil {
			state = c.ProviderState
		}
	}
	blocks, ok := state.([]map[string]any)
	if !ok || len(blocks) != 1 {
		t.Fatalf("ProviderState = %#v", state)
	}
	thinking, _ := blocks[0]["thinking"].(string)
	if thinking != "need a lookup" || blocks[0]["signature"] != "sig_2" {
		t.Fatalf("thinking block = %#v", blocks[0])
	}
}
