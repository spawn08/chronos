package model

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIAudio_Transcribe_DecodeError_Squeeze(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `not-json`)
	}))
	defer svr.Close()

	a := NewOpenAIAudioWithConfig(OpenAIAudioConfig{APIKey: "key", BaseURL: svr.URL})
	_, err := a.Transcribe(context.Background(), AudioContent{Data: []byte("x"), Format: "wav"})
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestOpenAIAudio_Synthesize_EmptyText_Squeeze(t *testing.T) {
	a := NewOpenAIAudio("key")
	_, err := a.Synthesize(context.Background(), "   ", "alloy")
	if err == nil {
		t.Fatal("expected error for whitespace-only text")
	}
}

func TestOpenAIAudio_Synthesize_DefaultVoice_Squeeze(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("audio"))
	}))
	defer svr.Close()

	a := NewOpenAIAudioWithConfig(OpenAIAudioConfig{APIKey: "key", BaseURL: svr.URL})
	ac, err := a.Synthesize(context.Background(), "hi", "")
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(ac.Data) == 0 {
		t.Fatal("expected audio bytes")
	}
}
