package model

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewOpenAIAudio_Defaults(t *testing.T) {
	a := NewOpenAIAudio("key")
	if a.transcribeModel != "whisper-1" {
		t.Errorf("transcribeModel=%q", a.transcribeModel)
	}
	if a.ttsModel != "tts-1" {
		t.Errorf("ttsModel=%q", a.ttsModel)
	}
	if a.ttsFormat != "mp3" {
		t.Errorf("ttsFormat=%q", a.ttsFormat)
	}
}

func TestNewOpenAIAudioWithConfig_OrgID(t *testing.T) {
	a := NewOpenAIAudioWithConfig(OpenAIAudioConfig{
		APIKey: "key",
		OrgID:  "org-123",
	})
	if a == nil {
		t.Fatal("expected non-nil")
	}
}

func TestNewOpenAIAudioWithConfig_CustomModels(t *testing.T) {
	a := NewOpenAIAudioWithConfig(OpenAIAudioConfig{
		APIKey:          "key",
		TranscribeModel: "whisper-2",
		TTSModel:        "tts-1-hd",
		TTSFormat:       "opus",
		TimeoutSec:      60,
	})
	if a.transcribeModel != "whisper-2" {
		t.Errorf("transcribeModel=%q", a.transcribeModel)
	}
	if a.ttsModel != "tts-1-hd" {
		t.Errorf("ttsModel=%q", a.ttsModel)
	}
}

func TestOpenAIAudio_Transcribe_AlreadySet(t *testing.T) {
	a := NewOpenAIAudio("key")
	result, err := a.Transcribe(context.Background(), AudioContent{
		Transcript: "already transcribed",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result != "already transcribed" {
		t.Errorf("result=%q", result)
	}
}

func TestOpenAIAudio_Transcribe_EmptyData(t *testing.T) {
	a := NewOpenAIAudio("key")
	_, err := a.Transcribe(context.Background(), AudioContent{})
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestOpenAIAudio_Transcribe_Success(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text":"hello world"}`)
	}))
	defer svr.Close()

	a := NewOpenAIAudioWithConfig(OpenAIAudioConfig{APIKey: "key", BaseURL: svr.URL})
	result, err := a.Transcribe(context.Background(), AudioContent{
		Data:   []byte("fake audio data"),
		Format: "wav",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if result != "hello world" {
		t.Errorf("result=%q", result)
	}
}

func TestOpenAIAudio_Transcribe_HTTPError(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"invalid api key"}`)
	}))
	defer svr.Close()

	a := NewOpenAIAudioWithConfig(OpenAIAudioConfig{APIKey: "bad", BaseURL: svr.URL})
	_, err := a.Transcribe(context.Background(), AudioContent{
		Data:   []byte("audio"),
		Format: "mp3",
	})
	if err == nil {
		t.Fatal("expected error for 401")
	}
}

func TestOpenAIAudio_Synthesize_Success(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fake audio bytes"))
	}))
	defer svr.Close()

	a := NewOpenAIAudioWithConfig(OpenAIAudioConfig{APIKey: "key", BaseURL: svr.URL})
	audio, err := a.Synthesize(context.Background(), "Hello, world!", "alloy")
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(audio.Data) == 0 {
		t.Error("expected audio data")
	}
}

func TestOpenAIAudio_Synthesize_HTTPError(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"invalid voice"}`)
	}))
	defer svr.Close()

	a := NewOpenAIAudioWithConfig(OpenAIAudioConfig{APIKey: "key", BaseURL: svr.URL})
	_, err := a.Synthesize(context.Background(), "test", "invalid-voice")
	if err == nil {
		t.Fatal("expected error for 400")
	}
}

func TestOpenAIAudio_Transcribe_NoFormat(t *testing.T) {
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"text":"transcribed"}`)
	}))
	defer svr.Close()

	// No format specified, should default to "wav"
	a := NewOpenAIAudioWithConfig(OpenAIAudioConfig{APIKey: "key", BaseURL: svr.URL})
	result, err := a.Transcribe(context.Background(), AudioContent{
		Data: []byte("fake audio"),
		// Format is empty
	})
	if err != nil {
		t.Fatalf("Transcribe no format: %v", err)
	}
	if result != "transcribed" {
		t.Errorf("result=%q", result)
	}
}
