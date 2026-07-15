package middleware

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Result is the outcome of a single rate-limit check.
type Result struct {
	// Allowed reports whether the current request is within the limit.
	Allowed bool
	// Remaining is the number of further requests permitted in the window (>=0).
	Remaining int
	// ResetAt is when the current fixed window resets.
	ResetAt time.Time
}

// Limiter is the pluggable backend for the rate-limit middleware. The default is
// an in-process counter (InProcessLimiter); a store-backed Limiter (SQLLimiter)
// shares counts across replicas so the limit holds cluster-wide.
type Limiter interface {
	// Allow records one hit for key within a fixed window of the given size and
	// reports whether the hit is within limit, the remaining allowance, and the
	// window reset time.
	Allow(ctx context.Context, key string, limit int, window time.Duration) (Result, error)
}

// RateLimitConfig holds configuration for the rate limiter.
type RateLimitConfig struct {
	RequestsPerWindow int
	Window            time.Duration
	KeyFunc           func(r *http.Request) string
	// Limiter selects the backend. A nil Limiter uses a per-middleware in-process
	// limiter (single-replica semantics; the historical default).
	Limiter Limiter
}

// DefaultRateLimitConfig returns a rate limiter that allows 100 requests
// per minute keyed by client IP.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerWindow: 100,
		Window:            time.Minute,
		KeyFunc:           IPKeyFunc,
	}
}

// IPKeyFunc extracts the client IP for rate limiting.
func IPKeyFunc(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

type bucket struct {
	count   int
	resetAt time.Time
}

// InProcessLimiter is an in-memory fixed-window counter. It is the default
// backend and is safe for concurrent use, but its counts are per-process: run
// multiple replicas and each enforces the limit independently. Use SQLLimiter
// for a shared, cluster-wide limit.
type InProcessLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	now     func() time.Time
}

// NewInProcessLimiter creates an in-process limiter.
func NewInProcessLimiter() *InProcessLimiter {
	return &InProcessLimiter{buckets: make(map[string]*bucket), now: time.Now}
}

// Allow implements Limiter.
func (l *InProcessLimiter) Allow(_ context.Context, key string, limit int, window time.Duration) (Result, error) {
	now := l.now()
	l.mu.Lock()
	b, ok := l.buckets[key]
	if !ok || now.After(b.resetAt) {
		b = &bucket{count: 0, resetAt: now.Add(window)}
		l.buckets[key] = b
	}
	b.count++
	count := b.count
	resetAt := b.resetAt
	l.mu.Unlock()

	return Result{
		Allowed:   count <= limit,
		Remaining: maxInt(0, limit-count),
		ResetAt:   resetAt,
	}, nil
}

// RateLimit returns middleware that limits requests using a fixed-window
// counter. The backend is cfg.Limiter, defaulting to a fresh in-process limiter.
func RateLimit(cfg RateLimitConfig) func(http.Handler) http.Handler {
	limiter := cfg.Limiter
	if limiter == nil {
		limiter = NewInProcessLimiter()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := cfg.KeyFunc(r)

			res, err := limiter.Allow(r.Context(), key, cfg.RequestsPerWindow, cfg.Window)
			if err != nil {
				// Fail open: a backend blip must not take down the whole API. The
				// request proceeds without rate-limit headers.
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("X-RateLimit-Limit", itoa(cfg.RequestsPerWindow))
			w.Header().Set("X-RateLimit-Remaining", itoa(res.Remaining))
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", res.ResetAt.Unix()))

			if !res.Allowed {
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(time.Until(res.ResetAt).Seconds())+1))
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Dialect selects SQL syntax for the SQLLimiter's target database.
type Dialect string

const (
	// DialectSQLite targets SQLite (dev/test). Writers are serialized, so the
	// read-modify-write in Allow is atomic without an explicit row lock.
	DialectSQLite Dialect = "sqlite"
	// DialectPostgres targets PostgreSQL (production). Allow locks the bucket row
	// FOR UPDATE so concurrent replicas serialize their counter updates.
	DialectPostgres Dialect = "postgres"
)

// SQLLimiter is a store-backed fixed-window limiter. It shares its counters in a
// database table so the limit holds across all replicas pointing at the same
// database, rather than per-process like InProcessLimiter. It implements Limiter.
type SQLLimiter struct {
	db      *sql.DB
	dialect Dialect
	now     func() time.Time
}

// NewSQLLimiter constructs a shared limiter over an already-open *sql.DB. Call
// Migrate before first use. The caller owns the DB lifecycle.
func NewSQLLimiter(db *sql.DB, dialect Dialect) *SQLLimiter {
	return &SQLLimiter{db: db, dialect: dialect, now: time.Now}
}

func (l *SQLLimiter) rebind(query string) string {
	if l.dialect != DialectPostgres {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Migrate creates the rate-limit schema.
func (l *SQLLimiter) Migrate(ctx context.Context) error {
	ts := "TIMESTAMP"
	if l.dialect == DialectPostgres {
		ts = "TIMESTAMPTZ"
	}
	stmt := `CREATE TABLE IF NOT EXISTS ratelimit_buckets (
		bucket_key TEXT PRIMARY KEY,
		count INTEGER NOT NULL,
		reset_at ` + ts + ` NOT NULL
	)`
	if _, err := l.db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("ratelimit migrate: %w", err)
	}
	return nil
}

// Allow implements Limiter with a shared fixed-window counter. The read (locked
// on Postgres), decide, and write happen in one transaction so concurrent
// replicas cannot both increment past the boundary.
func (l *SQLLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (Result, error) {
	now := l.now().UTC()
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, fmt.Errorf("ratelimit allow: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	lockClause := ""
	if l.dialect == DialectPostgres {
		lockClause = " FOR UPDATE"
	}

	var (
		count   int
		resetAt time.Time
	)
	err = tx.QueryRowContext(ctx, l.rebind(
		`SELECT count, reset_at FROM ratelimit_buckets WHERE bucket_key=?`+lockClause), key).
		Scan(&count, &resetAt)

	switch {
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		// A genuine read error: do not touch the shared counter (fail open at
		// the caller) rather than resetting it on corrupt/zero state.
		return Result{}, fmt.Errorf("ratelimit allow: read: %w", err)
	case errors.Is(err, sql.ErrNoRows) || now.After(resetAt):
		// Fresh or expired window: (re)start the counter at 1. Note: FOR UPDATE
		// cannot lock a not-yet-existing row, so at the very first request(s) of
		// a brand-new key, concurrent replicas may each see ErrNoRows and upsert
		// count=1 — a brief, bounded over-admission at window start only. The
		// existing-row path below is fully serialized by FOR UPDATE.
		count = 1
		resetAt = now.Add(window)
		if _, uErr := tx.ExecContext(ctx, l.rebind(
			`INSERT INTO ratelimit_buckets (bucket_key, count, reset_at) VALUES (?, ?, ?)
			 ON CONFLICT (bucket_key) DO UPDATE SET count=?, reset_at=?`),
			key, count, resetAt, count, resetAt); uErr != nil {
			return Result{}, fmt.Errorf("ratelimit allow: reset: %w", uErr)
		}
	default:
		count++
		if _, uErr := tx.ExecContext(ctx, l.rebind(
			`UPDATE ratelimit_buckets SET count=? WHERE bucket_key=?`), count, key); uErr != nil {
			return Result{}, fmt.Errorf("ratelimit allow: increment: %w", uErr)
		}
	}

	if err := tx.Commit(); err != nil {
		return Result{}, fmt.Errorf("ratelimit allow: commit: %w", err)
	}

	return Result{
		Allowed:   count <= limit,
		Remaining: maxInt(0, limit-count),
		ResetAt:   resetAt,
	}, nil
}
