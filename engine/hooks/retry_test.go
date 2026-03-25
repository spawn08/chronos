package hooks

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/model"
)

// mockProvider implements model.Provider for testing.
type mockProvider struct {
	responses []*model.ChatResponse
	errors    []error
	callCount atomic.Int32
}

func (m *mockProvider) Chat(_ context.Context, _ *model.ChatRequest) (*model.ChatResponse, error) {
	idx := int(m.callCount.Add(1)) - 1
	if idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return &model.ChatResponse{Content: "default"}, nil
}

func (m *mockProvider) StreamChat(_ context.Context, _ *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockProvider) Name() string  { return "mock" }
func (m *mockProvider) Model() string { return "mock-model" }

func noopSleep(_ time.Duration) {}

func TestRetryHook_SuccessfulRetry(t *testing.T) {
	// First retry call fails, second succeeds
	provider := &mockProvider{
		errors:    []error{errors.New("transient error"), nil},
		responses: []*model.ChatResponse{nil, {Content: "success on retry"}},
	}

	hook := NewRetryHook(3)
	hook.SleepFn = noopSleep

	req := &model.ChatRequest{Messages: []model.Message{{Role: "user", Content: "hello"}}}
	evt := &Event{
		Type:  EventModelCallAfter,
		Error: errors.New("transient error"),
		Metadata: map[string]any{
			"provider": model.Provider(provider),
			"request":  req,
		},
	}

	err := hook.After(context.Background(), evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.Error != nil {
		t.Fatalf("expected error to be cleared, got: %v", evt.Error)
	}

	resp, ok := evt.Output.(*model.ChatResponse)
	if !ok || resp == nil {
		t.Fatal("expected output to be *model.ChatResponse")
	}
	if resp.Content != "success on retry" {
		t.Errorf("content = %q, want %q", resp.Content, "success on retry")
	}
	// Attempt 1 fails, attempt 2 succeeds = 2 retries counted
	if hook.Retries != 2 {
		t.Errorf("retries = %d, want 2", hook.Retries)
	}
	if attempts, ok := evt.Metadata["retry_attempts"].(int); !ok || attempts != 2 {
		t.Errorf("retry_attempts = %v, want 2", evt.Metadata["retry_attempts"])
	}
}

func TestRetryHook_ExhaustedRetries(t *testing.T) {
	permanentErr := errors.New("permanent error")
	provider := &mockProvider{
		errors: []error{permanentErr, permanentErr, permanentErr},
	}

	hook := NewRetryHook(3)
	hook.SleepFn = noopSleep

	req := &model.ChatRequest{Messages: []model.Message{{Role: "user", Content: "hello"}}}
	evt := &Event{
		Type:  EventModelCallAfter,
		Error: permanentErr,
		Metadata: map[string]any{
			"provider": model.Provider(provider),
			"request":  req,
		},
	}

	err := hook.After(context.Background(), evt)
	if err != nil {
		t.Fatalf("hook should not return error itself: %v", err)
	}
	if evt.Error == nil {
		t.Fatal("expected error to remain after exhausted retries")
	}
	if hook.Retries != 3 {
		t.Errorf("retries = %d, want 3", hook.Retries)
	}
	if success, ok := evt.Metadata["retry_success"].(bool); !ok || success {
		t.Error("retry_success should be false")
	}
}

func TestRetryHook_NonRetryableError(t *testing.T) {
	provider := &mockProvider{
		errors: []error{errors.New("auth error")},
	}

	hook := NewRetryHook(3)
	hook.SleepFn = noopSleep
	hook.RetryableError = func(err error) bool {
		return err.Error() != "auth error"
	}

	req := &model.ChatRequest{}
	evt := &Event{
		Type:  EventModelCallAfter,
		Error: errors.New("auth error"),
		Metadata: map[string]any{
			"provider": model.Provider(provider),
			"request":  req,
		},
	}

	_ = hook.After(context.Background(), evt)
	if hook.Retries != 0 {
		t.Errorf("retries = %d, want 0 (non-retryable error)", hook.Retries)
	}
}

func TestRetryHook_ContextCancelled(t *testing.T) {
	provider := &mockProvider{
		errors: []error{errors.New("error"), errors.New("error")},
	}

	hook := NewRetryHook(3)
	canceled := false
	hook.SleepFn = func(_ time.Duration) {
		if !canceled {
			canceled = true
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	hook.SleepFn = func(_ time.Duration) {
		cancel()
	}

	req := &model.ChatRequest{}
	evt := &Event{
		Type:  EventModelCallAfter,
		Error: errors.New("error"),
		Metadata: map[string]any{
			"provider": model.Provider(provider),
			"request":  req,
		},
	}

	err := hook.After(ctx, evt)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestRetryHook_IgnoresNonModelCallEvents(t *testing.T) {
	hook := NewRetryHook(3)
	hook.SleepFn = noopSleep

	evt := &Event{
		Type:  EventToolCallAfter,
		Error: errors.New("tool error"),
	}

	err := hook.After(context.Background(), evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hook.Retries != 0 {
		t.Errorf("retries = %d, want 0", hook.Retries)
	}
}

func TestRetryHook_IgnoresSuccessfulCalls(t *testing.T) {
	hook := NewRetryHook(3)
	hook.SleepFn = noopSleep

	evt := &Event{
		Type:  EventModelCallAfter,
		Error: nil,
	}

	err := hook.After(context.Background(), evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hook.Retries != 0 {
		t.Errorf("retries = %d, want 0", hook.Retries)
	}
}

func TestRetryHook_OnRetryCallback(t *testing.T) {
	// 3 calls: err, err, success
	provider := &mockProvider{
		errors:    []error{errors.New("err1"), errors.New("err2"), nil},
		responses: []*model.ChatResponse{nil, nil, {Content: "ok"}},
	}

	hook := NewRetryHook(3)
	hook.SleepFn = noopSleep

	var attempts []int
	var delays []time.Duration
	hook.OnRetry = func(attempt int, delay time.Duration) {
		attempts = append(attempts, attempt)
		delays = append(delays, delay)
	}

	req := &model.ChatRequest{}
	evt := &Event{
		Type:  EventModelCallAfter,
		Error: errors.New("err1"),
		Metadata: map[string]any{
			"provider": model.Provider(provider),
			"request":  req,
		},
	}

	_ = hook.After(context.Background(), evt)
	if len(attempts) != 3 {
		t.Fatalf("OnRetry called %d times, want 3", len(attempts))
	}
	if attempts[0] != 1 || attempts[1] != 2 || attempts[2] != 3 {
		t.Errorf("attempts = %v, want [1, 2, 3]", attempts)
	}
	if delays[1] <= delays[0] {
		t.Errorf("expected exponential backoff: delay[1]=%v <= delay[0]=%v", delays[1], delays[0])
	}
}

func TestRetryHook_FallbackSignalMode(t *testing.T) {
	hook := NewRetryHook(3)
	hook.SleepFn = noopSleep

	evt := &Event{
		Type:     EventModelCallAfter,
		Error:    errors.New("error"),
		Metadata: map[string]any{},
	}

	_ = hook.After(context.Background(), evt)

	if retry, ok := evt.Metadata["retry"].(bool); !ok || !retry {
		t.Error("expected retry=true in fallback mode")
	}
	if attempt, ok := evt.Metadata["retry_attempt"].(int); !ok || attempt != 2 {
		t.Errorf("retry_attempt = %v, want 2", evt.Metadata["retry_attempt"])
	}
	if _, ok := evt.Metadata["retry_delay"].(time.Duration); !ok {
		t.Error("expected retry_delay to be set")
	}
}

func TestRetryHook_DefaultMaxRetries(t *testing.T) {
	hook := NewRetryHook(0)
	if hook.MaxRetries != 3 {
		t.Errorf("default max retries = %d, want 3", hook.MaxRetries)
	}
	hook2 := NewRetryHook(-1)
	if hook2.MaxRetries != 3 {
		t.Errorf("negative max retries should default to 3, got %d", hook2.MaxRetries)
	}
}

func TestRetryHook_BackoffInRange(t *testing.T) {
	hook := NewRetryHook(5)
	for attempt := 1; attempt <= 5; attempt++ {
		delay := hook.backoff(attempt)
		if delay < 0 {
			t.Errorf("attempt %d: negative delay %v", attempt, delay)
		}
		if delay > hook.MaxDelay {
			t.Errorf("attempt %d: delay %v > max %v", attempt, delay, hook.MaxDelay)
		}
	}
}

func TestRetryHook_BackoffDefaults(t *testing.T) {
	// Test backoff with zero BaseDelay and MaxDelay (should use defaults)
	hook := &RetryHook{MaxRetries: 3, BaseDelay: 0, MaxDelay: 0}
	delay := hook.backoff(1)
	if delay < 0 {
		t.Errorf("expected non-negative delay, got %v", delay)
	}
	// Should use defaults: base=500ms, max=30s
	if delay > 30*time.Second {
		t.Errorf("delay %v exceeds 30s default max", delay)
	}
}

func TestRetryHook_BeforeIsNoop(t *testing.T) {
	hook := NewRetryHook(3)
	err := hook.Before(context.Background(), &Event{})
	if err != nil {
		t.Fatalf("Before should return nil, got: %v", err)
	}
}

func TestRetryHook_SuccessOnSecondAttempt(t *testing.T) {
	// Attempt 1 (in retry) fails, attempt 2 succeeds
	provider := &mockProvider{
		errors:    []error{errors.New("fail1"), nil},
		responses: []*model.ChatResponse{nil, {Content: "second attempt success", Role: "assistant"}},
	}

	hook := NewRetryHook(5)
	hook.SleepFn = noopSleep

	req := &model.ChatRequest{}
	evt := &Event{
		Type:  EventModelCallAfter,
		Error: errors.New("fail1"),
		Metadata: map[string]any{
			"provider": model.Provider(provider),
			"request":  req,
		},
	}

	_ = hook.After(context.Background(), evt)
	if evt.Error != nil {
		t.Fatalf("expected no error after successful retry, got: %v", evt.Error)
	}
	resp := evt.Output.(*model.ChatResponse)
	if resp.Content != "second attempt success" {
		t.Errorf("content = %q, want %q", resp.Content, "second attempt success")
	}
	// 2 retry attempts executed
	if hook.Retries != 2 {
		t.Errorf("retries = %d, want 2", hook.Retries)
	}
}

func TestRetryHook_SignalRetry_ExceedsMaxRetries(t *testing.T) {
	hook := NewRetryHook(2)
	hook.SleepFn = noopSleep

	// Set attempt to max+1 in metadata - signalRetry should return without retrying
	evt := &Event{
		Type:  EventModelCallAfter,
		Error: errors.New("error"),
		Metadata: map[string]any{
			"retry_attempt": 3, // exceeds MaxRetries=2
		},
	}
	_ = hook.After(context.Background(), evt)
	// Should not increment Retries since attempt > MaxRetries
	if hook.Retries != 0 {
		t.Errorf("expected 0 retries when attempt exceeds max, got %d", hook.Retries)
	}
}
