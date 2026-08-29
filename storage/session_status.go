package storage

import (
	"context"
	"time"
)

// SessionStatusUpdater is an OPTIONAL interface a Storage adapter may
// implement to update a session's Status (and UpdatedAt) as a narrow, targeted
// write instead of a whole-record GetSession -> mutate -> UpdateSession
// read-modify-write. Sessions already have another read-modify-write writer of
// their own (a plan/VFS Metadata writer scoped by storage.WithSession), so a
// caller that only wants to flip Status — e.g. the graph runner mirroring a
// run's paused/completed/failed status — would otherwise risk silently
// clobbering a concurrent Metadata write with the stale copy it read moments
// earlier. A narrow update can never do that, because it never touches
// Metadata.
//
// Callers should type-assert and fall back to GetSession/UpdateSession when
// the store doesn't implement it (accepting the pre-existing race) so this
// stays additive: an adapter that doesn't implement SessionStatusUpdater is
// otherwise unaffected.
//
//	if su, ok := store.(storage.SessionStatusUpdater); ok {
//		_ = su.UpdateSessionStatus(ctx, sessionID, status, time.Now())
//	}
type SessionStatusUpdater interface {
	// UpdateSessionStatus sets the session's Status and UpdatedAt. It returns
	// an error if the session does not exist (or is not visible to the
	// context's tenant), mirroring GetSession's not-found behavior.
	UpdateSessionStatus(ctx context.Context, sessionID, status string, updatedAt time.Time) error
}
