package model

import (
	"context"
	"encoding/json"
	"testing"
)

func TestProviderUsagePresenceDistinguishesExplicitZeroFromMissing(t *testing.T) {
	for _, raw := range []struct {
		name  string
		body  string
		known bool
	}{
		{name: "openai zero", body: `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":0,"completion_tokens":0}}`, known: true},
		{name: "openai missing", body: `{"choices":[{"message":{"content":"ok"}}]}`},
	} {
		t.Run(raw.name, func(t *testing.T) {
			var parsed openAIChatResponse
			if err := json.Unmarshal([]byte(raw.body), &parsed); err != nil {
				t.Fatal(err)
			}
			if got := convertOpenAIResponse(&parsed).UsageKnown; got != raw.known {
				t.Fatalf("UsageKnown = %v, want %v", got, raw.known)
			}
		})
	}
	for _, raw := range []struct {
		name  string
		body  string
		known bool
	}{
		{name: "anthropic zero", body: `{"content":[],"usage":{"input_tokens":0,"output_tokens":0}}`, known: true},
		{name: "anthropic missing", body: `{"content":[]}`},
	} {
		t.Run(raw.name, func(t *testing.T) {
			var parsed anthropicResponse
			if err := json.Unmarshal([]byte(raw.body), &parsed); err != nil {
				t.Fatal(err)
			}
			if got := NewAnthropic("test").convertResponse(&parsed).UsageKnown; got != raw.known {
				t.Fatalf("UsageKnown = %v, want %v", got, raw.known)
			}
		})
	}
	stream := make(chan *ChatResponse, 1)
	stream <- &ChatResponse{UsageKnown: true, Usage: Usage{}, Delta: true}
	close(stream)
	if result, err := AggregateStream(context.Background(), stream); err != nil || !result.UsageKnown {
		t.Fatalf("stream zero usage = %+v, error = %v", result, err)
	}
}
