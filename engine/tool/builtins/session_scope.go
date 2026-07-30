package builtins

import (
	"context"
	"errors"

	"github.com/spawn08/chronos/storage"
)

// ErrNoSession is returned by any session-scoped harness primitive (the planning
// tool's PlanStore, the virtual filesystem's VFS) when the context carries no
// session id. These primitives are inherently per-session, so a sessionless call
// is a programming error, not an empty result — use storage.WithSession,
// ChatWithSession, or a graph run. The message is deliberately generic so it
// reads correctly from whichever primitive surfaced it.
var ErrNoSession = errors.New("harness: no active session in context (use storage.WithSession)")

// sessionScope returns the session id from ctx, or ErrNoSession when none is set.
// It is the single gate every session-scoped harness store shares, so they all
// reject a sessionless context identically (LSP).
func sessionScope(ctx context.Context) (string, error) {
	if sessionID := storage.SessionFromContext(ctx); sessionID != "" {
		return sessionID, nil
	}
	return "", ErrNoSession
}
