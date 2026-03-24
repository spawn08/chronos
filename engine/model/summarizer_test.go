package model

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type mockSummarizerProvider struct {
	response string
	err      error
}

func (p *mockSummarizerProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	if p.err != nil {
		return nil, p.err
	}
	return &ChatResponse{Content: p.response}, nil
}

func (p *mockSummarizerProvider) StreamChat(_ context.Context, _ *ChatRequest) (<-chan *ChatResponse, error) {
	return nil, errors.New("not implemented")
}

func (p *mockSummarizerProvider) Name() string  { return "mock" }
func (p *mockSummarizerProvider) Model() string { return "mock" }

func TestNeedsSummarization_UnderThreshold(t *testing.T) {
	s := NewSummarizer(
		&mockSummarizerProvider{response: "summary"},
		NewEstimatingCounter(),
		SummarizationConfig{Threshold: 0.8, PreserveRecentTurns: 5},
	)

	msgs := []Message{{Role: RoleUser, Content: "hello"}}
	if s.NeedsSummarization(10, msgs, 100000) {
		t.Error("should not need summarization for small messages")
	}
}

func TestNeedsSummarization_OverThreshold(t *testing.T) {
	s := NewSummarizer(
		&mockSummarizerProvider{response: "summary"},
		NewEstimatingCounter(),
		SummarizationConfig{Threshold: 0.1, PreserveRecentTurns: 1},
	)

	msgs := make([]Message, 100)
	for i := range msgs {
		msgs[i] = Message{Role: RoleUser, Content: strings.Repeat("word ", 200)}
	}

	if !s.NeedsSummarization(0, msgs, 100) {
		t.Error("should need summarization for large messages")
	}
}

func TestSummarize_Empty(t *testing.T) {
	s := NewSummarizer(
		&mockSummarizerProvider{response: "summary"},
		NewEstimatingCounter(),
		SummarizationConfig{Threshold: 0.8, PreserveRecentTurns: 5},
	)

	result, err := s.Summarize(context.Background(), "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.PreservedMessages) != 0 {
		t.Error("should preserve no messages for empty input")
	}
}

func TestSummarize_TooFewMessages(t *testing.T) {
	s := NewSummarizer(
		&mockSummarizerProvider{response: "summary"},
		NewEstimatingCounter(),
		SummarizationConfig{Threshold: 0.8, PreserveRecentTurns: 5},
	)

	msgs := []Message{
		{Role: RoleUser, Content: "hello"},
		{Role: RoleAssistant, Content: "hi"},
	}

	result, err := s.Summarize(context.Background(), "", msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.PreservedMessages) != 2 {
		t.Error("should preserve all messages when count < 2*preserveRecent")
	}
	if result.SummarizedCount != 0 {
		t.Error("nothing should be summarized")
	}
}

func TestSummarize_CompressesOlderMessages(t *testing.T) {
	s := NewSummarizer(
		&mockSummarizerProvider{response: "The user asked about Go."},
		NewEstimatingCounter(),
		SummarizationConfig{Threshold: 0.8, PreserveRecentTurns: 1},
	)

	msgs := []Message{
		{Role: RoleUser, Content: "What is Go?"},
		{Role: RoleAssistant, Content: "Go is a programming language."},
		{Role: RoleUser, Content: "How old is it?"},
		{Role: RoleAssistant, Content: "Since 2009."},
		{Role: RoleUser, Content: "Who created it?"},
		{Role: RoleAssistant, Content: "Google engineers."},
	}

	result, err := s.Summarize(context.Background(), "", msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SummarizedCount != 4 {
		t.Errorf("summarized count = %d, want 4", result.SummarizedCount)
	}
	if len(result.PreservedMessages) != 2 {
		t.Errorf("preserved count = %d, want 2", len(result.PreservedMessages))
	}
	if result.Summary != "The user asked about Go." {
		t.Errorf("summary = %q", result.Summary)
	}
}

func TestSummarize_WithExistingSummary(t *testing.T) {
	s := NewSummarizer(
		&mockSummarizerProvider{response: "Extended summary including previous context."},
		NewEstimatingCounter(),
		SummarizationConfig{Threshold: 0.8, PreserveRecentTurns: 1},
	)

	msgs := []Message{
		{Role: RoleUser, Content: "Q1"},
		{Role: RoleAssistant, Content: "A1"},
		{Role: RoleUser, Content: "Q2"},
		{Role: RoleAssistant, Content: "A2"},
	}

	result, err := s.Summarize(context.Background(), "Previous chat about Go", msgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Summary, "Extended summary") {
		t.Errorf("summary should incorporate provider response: %q", result.Summary)
	}
}

func TestSummarize_ProviderError(t *testing.T) {
	s := NewSummarizer(
		&mockSummarizerProvider{err: errors.New("model down")},
		NewEstimatingCounter(),
		SummarizationConfig{Threshold: 0.8, PreserveRecentTurns: 1},
	)

	msgs := []Message{
		{Role: RoleUser, Content: "Q1"},
		{Role: RoleAssistant, Content: "A1"},
		{Role: RoleUser, Content: "Q2"},
		{Role: RoleAssistant, Content: "A2"},
	}

	_, err := s.Summarize(context.Background(), "", msgs)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEstimateTokens(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "hello world"},
		{Role: RoleAssistant, Content: "hi there"},
	}
	tokens := EstimateTokens(msgs)
	if tokens <= 0 {
		t.Error("token estimate should be positive")
	}
}

func TestSummarizationConfig_Defaults(t *testing.T) {
	s := NewSummarizer(
		&mockSummarizerProvider{response: "ok"},
		NewEstimatingCounter(),
		SummarizationConfig{},
	)
	if s.config.Threshold != 0.8 {
		t.Errorf("default threshold = %f, want 0.8", s.config.Threshold)
	}
	if s.config.PreserveRecentTurns != 5 {
		t.Errorf("default preserveRecent = %d, want 5", s.config.PreserveRecentTurns)
	}
}
