package storage

import "context"

// sessionContextKey is the private context key under which the current session
// id is stored. An unexported struct type avoids collisions with keys set by
// other packages.
type sessionContextKey struct{}

// WithSession returns a copy of ctx that carries the given session id. Harness
// primitives that persist per-session state (e.g. the planning tool and the
// virtual filesystem) read this id to scope their reads and writes to a single
// session. An empty id is a no-op and leaves the context unchanged.
//
// This is intentionally shaped like WithTenant: session scoping composes with
// tenant scoping, so a fully scoped context carries both.
func WithSession(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionContextKey{}, sessionID)
}

// SessionFromContext returns the session id carried by ctx, or the empty string
// when none has been set. Unlike TenantFromContext there is no default: a caller
// that needs a session id must treat the empty string as "no active session".
func SessionFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(sessionContextKey{}).(string); ok {
		return v
	}
	return ""
}
