package model

import (
	"context"
	"time"
)

type retriesDisabledKey struct{}

// WithRetriesDisabled bounds a billable provider operation to one transport
// attempt, including fallback providers. A durable owner can then reconcile
// that attempt before it decides whether another call is authorized.
func WithRetriesDisabled(ctx context.Context) context.Context {
	return context.WithValue(ctx, retriesDisabledKey{}, true)
}

func retriesDisabled(ctx context.Context) bool {
	return ctx.Value(retriesDisabledKey{}) == true
}

// RetryInfo describes a transient provider failure that is about to be
// retried after Delay.
type RetryInfo struct {
	// Attempt is the 1-based number of the retry about to run.
	Attempt int
	// Delay is how long the caller waits before retrying.
	Delay time.Duration
	// StatusCode is the HTTP status that triggered the retry, or 0 for a
	// transport failure.
	StatusCode int
	// Err is the transport or provider error, when there is one.
	Err error
}

// RetryObserver is told about each retry before its wait starts, so a UI can
// explain a pause instead of looking stalled. It must not block.
type RetryObserver func(RetryInfo)

type retryObserverKey struct{}

// WithRetryObserver attaches observer to provider calls made with ctx.
func WithRetryObserver(ctx context.Context, observer RetryObserver) context.Context {
	if observer == nil {
		return ctx
	}
	return context.WithValue(ctx, retryObserverKey{}, observer)
}

// NotifyRetry reports info to the observer attached to ctx, if any.
func NotifyRetry(ctx context.Context, info RetryInfo) {
	if observer, ok := ctx.Value(retryObserverKey{}).(RetryObserver); ok {
		observer(info)
	}
}
