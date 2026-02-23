// Package redis provides a Redis-backed Storage adapter stub for session/cache use cases.
package redis

// Store will implement storage.Storage using Redis.
// Primarily intended for session caching and short-term state.
type Store struct {
	Addr     string
	Password string
	DB       int
}

// TODO: Implement storage.Storage interface methods.
// Redis is best suited for session caching alongside a primary SQL store.
