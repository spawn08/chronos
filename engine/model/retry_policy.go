package model

import "context"

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
