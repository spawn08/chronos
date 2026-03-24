package hooks

import (
	"context"
	"errors"
	"testing"

	"github.com/spawn08/chronos/engine/model"
)

func TestRetryHook_StopsWhenRetryBecomesNonRetryable(t *testing.T) {
	provider := &mockProvider{
		errors: []error{
			errors.New("temporary"),
			errors.New("permanent-auth"),
		},
	}
	hook := NewRetryHook(5)
	hook.SleepFn = noopSleep
	hook.RetryableError = func(err error) bool {
		return err.Error() != "permanent-auth"
	}

	req := &model.ChatRequest{}
	evt := &Event{
		Type:  EventModelCallAfter,
		Error: errors.New("temporary"),
		Metadata: map[string]any{
			"provider": model.Provider(provider),
			"request":  req,
		},
	}

	_ = hook.After(context.Background(), evt)
	if evt.Error == nil {
		t.Fatal("expected error to remain after non-retryable failure")
	}
	if evt.Error.Error() != "permanent-auth" {
		t.Errorf("got %v", evt.Error)
	}
}
