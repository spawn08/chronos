package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

// AudioProvider is the interface for audio transcription and synthesis.
type AudioProvider interface {
	// Transcribe converts audio data to text using a speech-to-text model (e.g., Whisper).
	Transcribe(ctx context.Context, audio AudioContent) (string, error)
	// Synthesize converts text to speech using a TTS model.
	// voice is the voice ID (e.g., "alloy", "echo", "fable", "onyx", "nova", "shimmer").
	Synthesize(ctx context.Context, text, voice string) (*AudioContent, error)
}

// OpenAIAudio implements AudioProvider for OpenAI's Whisper transcription and TTS endpoints.
type OpenAIAudio struct {
	config          ProviderConfig
	transcribeModel string // e.g., "whisper-1"
	ttsModel        string // e.g., "tts-1", "tts-1-hd"
	ttsFormat       string // e.g., "mp3", "opus", "aac", "flac", "wav", "pcm"
	http            *http.Client
	baseURL         string
	headers         map[string]string
}

// OpenAIAudioConfig holds configuration for the OpenAI audio provider.
type OpenAIAudioConfig struct {
	// APIKey is the OpenAI API key.
	APIKey string `json:"api_key"`
	// BaseURL overrides the default OpenAI base URL.
	BaseURL string `json:"base_url,omitempty"`
	// OrgID is the optional OpenAI organization ID.
	OrgID string `json:"org_id,omitempty"`
	// TranscribeModel is the Whisper model to use for transcription (default: "whisper-1").
	TranscribeModel string `json:"transcribe_model,omitempty"`
	// TTSModel is the TTS model to use for speech synthesis (default: "tts-1").
	TTSModel string `json:"tts_model,omitempty"`
	// TTSFormat is the output audio format for TTS (default: "mp3").
	// Supported: "mp3", "opus", "aac", "flac", "wav", "pcm".
	TTSFormat string `json:"tts_format,omitempty"`
	// TimeoutSec is the HTTP request timeout in seconds (default: 120).
	TimeoutSec int `json:"timeout_sec,omitempty"`
}

// NewOpenAIAudio creates an OpenAIAudio provider with the given API key and default models.
func NewOpenAIAudio(apiKey string) *OpenAIAudio {
	return NewOpenAIAudioWithConfig(OpenAIAudioConfig{
		APIKey: apiKey,
	})
}

// NewOpenAIAudioWithConfig creates an OpenAIAudio provider with full configuration.
func NewOpenAIAudioWithConfig(cfg OpenAIAudioConfig) *OpenAIAudio {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.TranscribeModel == "" {
		cfg.TranscribeModel = "whisper-1"
	}
	if cfg.TTSModel == "" {
		cfg.TTSModel = "tts-1"
	}
	if cfg.TTSFormat == "" {
		cfg.TTSFormat = "mp3"
	}
	timeoutSec := cfg.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	headers := map[string]string{
		"Authorization": "Bearer " + cfg.APIKey,
	}
	if cfg.OrgID != "" {
		headers["OpenAI-Organization"] = cfg.OrgID
	}
	return &OpenAIAudio{
		config: ProviderConfig{
			APIKey:     cfg.APIKey,
			BaseURL:    cfg.BaseURL,
			OrgID:      cfg.OrgID,
			TimeoutSec: timeoutSec,
		},
		transcribeModel: cfg.TranscribeModel,
		ttsModel:        cfg.TTSModel,
		ttsFormat:       cfg.TTSFormat,
		http: &http.Client{
			Timeout: time.Duration(timeoutSec) * time.Second,
		},
		baseURL: cfg.BaseURL,
		headers: headers,
	}
}

// Transcribe sends audio data to the OpenAI Whisper transcription endpoint and returns
// the transcribed text. audio.Format is used as the filename extension (e.g., "wav", "mp3").
// If audio.Transcript is already set it is returned immediately without an API call.
func (o *OpenAIAudio) Transcribe(ctx context.Context, audio AudioContent) (string, error) {
	if audio.Transcript != "" {
		return audio.Transcript, nil
	}
	if len(audio.Data) == 0 {
		return "", fmt.Errorf("openai audio transcribe: audio data is empty")
	}

	format := audio.Format
	if format == "" {
		format = "wav"
	}

	// Build multipart/form-data body.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	// Add the audio file field.
	fileFieldName := "file"
	filename := "audio." + format
	mimeType := "audio/" + format
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, fileFieldName, filename))
	h.Set("Content-Type", mimeType)
	fw, err := mw.CreatePart(h)
	if err != nil {
		return "", fmt.Errorf("openai audio transcribe: create file part: %w", err)
	}
	if _, err = fw.Write(audio.Data); err != nil {
		return "", fmt.Errorf("openai audio transcribe: write audio data: %w", err)
	}

	// Add the model field.
	if err = mw.WriteField("model", o.transcribeModel); err != nil {
		return "", fmt.Errorf("openai audio transcribe: write model field: %w", err)
	}

	// Add response_format field to get plain text back.
	if err = mw.WriteField("response_format", "json"); err != nil {
		return "", fmt.Errorf("openai audio transcribe: write response_format field: %w", err)
	}

	if err = mw.Close(); err != nil {
		return "", fmt.Errorf("openai audio transcribe: close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/audio/transcriptions", &buf)
	if err != nil {
		return "", fmt.Errorf("openai audio transcribe: create request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	for k, v := range o.headers {
		req.Header.Set(k, v)
	}

	resp, err := o.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai audio transcribe: http request: %w", err)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai audio transcribe: %s", readErrorBody(resp))
	}

	var result openAITranscriptionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("openai audio transcribe: decode response: %w", err)
	}

	return result.Text, nil
}

// Synthesize sends text to the OpenAI TTS endpoint and returns an AudioContent with
// the synthesized audio bytes. voice should be one of: "alloy", "echo", "fable",
// "onyx", "nova", "shimmer". An empty voice defaults to "alloy".
func (o *OpenAIAudio) Synthesize(ctx context.Context, text, voice string) (*AudioContent, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("openai audio synthesize: text is empty")
	}
	if voice == "" {
		voice = "alloy"
	}

	body := openAITTSRequest{
		Model:          o.ttsModel,
		Input:          text,
		Voice:          voice,
		ResponseFormat: o.ttsFormat,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai audio synthesize: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/audio/speech", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("openai audio synthesize: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range o.headers {
		req.Header.Set(k, v)
	}

	resp, err := o.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai audio synthesize: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai audio synthesize: %s", readErrorBody(resp))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai audio synthesize: read response: %w", err)
	}

	return &AudioContent{
		Data:   data,
		Format: o.ttsFormat,
	}, nil
}

// openAITranscriptionResponse is the JSON body returned by /v1/audio/transcriptions.
type openAITranscriptionResponse struct {
	Text string `json:"text"`
}

// openAITTSRequest is the JSON body sent to /v1/audio/speech.
type openAITTSRequest struct {
	Model          string `json:"model"`
	Input          string `json:"input"`
	Voice          string `json:"voice"`
	ResponseFormat string `json:"response_format,omitempty"`
}
