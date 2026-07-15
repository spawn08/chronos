package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestRateLimit_AllowsWithinLimit(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerWindow: 5,
		Window:            time.Minute,
		KeyFunc:           func(_ *http.Request) string { return "test" },
	}
	handler := RateLimit(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", http.NoBody)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("request %d: got status %d, want 200", i+1, rec.Code)
		}
	}
}

func TestRateLimit_BlocksExcess(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerWindow: 2,
		Window:            time.Minute,
		KeyFunc:           func(_ *http.Request) string { return "test" },
	}
	handler := RateLimit(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", http.NoBody)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("got status %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("X-RateLimit-Remaining"); got != "0" {
		t.Errorf("got remaining=%q, want 0", got)
	}
}

func TestRateLimit_DifferentKeys(t *testing.T) {
	callCount := 0
	cfg := RateLimitConfig{
		RequestsPerWindow: 1,
		Window:            time.Minute,
		KeyFunc: func(r *http.Request) string {
			return r.Header.Get("X-Client")
		},
	}
	handler := RateLimit(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount++
			w.WriteHeader(http.StatusOK)
		}),
	)

	req1 := httptest.NewRequest("GET", "/test", http.NoBody)
	req1.Header.Set("X-Client", "client-a")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest("GET", "/test", http.NoBody)
	req2.Header.Set("X-Client", "client-b")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if callCount != 2 {
		t.Errorf("got %d calls, want 2 (different keys)", callCount)
	}
}

func TestRateLimit_SetsHeaders(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerWindow: 10,
		Window:            time.Minute,
		KeyFunc:           func(_ *http.Request) string { return "test" },
	}
	handler := RateLimit(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-RateLimit-Limit"); got != "10" {
		t.Errorf("got limit=%q, want 10", got)
	}
	if got := rec.Header().Get("X-RateLimit-Remaining"); got != "9" {
		t.Errorf("got remaining=%q, want 9", got)
	}
	if got := rec.Header().Get("X-RateLimit-Reset"); got == "" {
		t.Error("missing reset header")
	}
}

func TestIPKeyFunc(t *testing.T) {
	req := httptest.NewRequest("GET", "/", http.NoBody)
	req.RemoteAddr = "192.168.1.1:12345"
	if got := IPKeyFunc(req); got != "192.168.1.1" {
		t.Errorf("got %q, want 192.168.1.1", got)
	}

	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	if got := IPKeyFunc(req); got != "10.0.0.1" {
		t.Errorf("got %q, want 10.0.0.1", got)
	}
}

func TestDefaultRateLimitConfig(t *testing.T) {
	cfg := DefaultRateLimitConfig()
	if cfg.RequestsPerWindow != 100 {
		t.Errorf("RequestsPerWindow=%d, want 100", cfg.RequestsPerWindow)
	}
	if cfg.Window != time.Minute {
		t.Errorf("Window=%v, want 1m", cfg.Window)
	}
	if cfg.KeyFunc == nil {
		t.Error("KeyFunc should not be nil")
	}
}

// --- store-backed (shared) limiter ---

func sqlLimiterDSN(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ratelimit.db")
	return path + "?_busy_timeout=5000&_journal_mode=WAL&_txlock=immediate"
}

func openSQLLimiter(t *testing.T, dsn string) *SQLLimiter {
	t.Helper()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })
	return NewSQLLimiter(db, DialectSQLite)
}

func TestSQLLimiter_Allow(t *testing.T) {
	lim := openSQLLimiter(t, sqlLimiterDSN(t))
	ctx := context.Background()
	if err := lim.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	const limit = 3
	tests := []struct {
		hit           int
		wantAllowed   bool
		wantRemaining int
	}{
		{1, true, 2},
		{2, true, 1},
		{3, true, 0},
		{4, false, 0},
		{5, false, 0},
	}
	for _, tt := range tests {
		res, err := lim.Allow(ctx, "k", limit, time.Minute)
		if err != nil {
			t.Fatalf("hit %d: Allow: %v", tt.hit, err)
		}
		if res.Allowed != tt.wantAllowed {
			t.Errorf("hit %d: Allowed=%v, want %v", tt.hit, res.Allowed, tt.wantAllowed)
		}
		if res.Remaining != tt.wantRemaining {
			t.Errorf("hit %d: Remaining=%d, want %d", tt.hit, res.Remaining, tt.wantRemaining)
		}
	}
}

func TestSQLLimiter_WindowReset(t *testing.T) {
	lim := openSQLLimiter(t, sqlLimiterDSN(t))
	ctx := context.Background()
	if err := lim.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Drive time explicitly so the window boundary is deterministic.
	base := time.Now()
	lim.now = func() time.Time { return base }
	if res, _ := lim.Allow(ctx, "k", 1, time.Minute); !res.Allowed {
		t.Fatal("first hit should be allowed")
	}
	if res, _ := lim.Allow(ctx, "k", 1, time.Minute); res.Allowed {
		t.Fatal("second hit in window should be blocked")
	}
	// Advance past the window; the counter resets.
	lim.now = func() time.Time { return base.Add(2 * time.Minute) }
	if res, _ := lim.Allow(ctx, "k", 1, time.Minute); !res.Allowed {
		t.Fatal("hit after window reset should be allowed")
	}
}

// TestSQLLimiter_SharedAcrossInstances proves the limit holds across replicas:
// two limiter instances over independent DB handles share one counter, so their
// combined hits are capped by the shared limit. Run with -race.
func TestSQLLimiter_SharedAcrossInstances(t *testing.T) {
	dsn := sqlLimiterDSN(t)
	limA := openSQLLimiter(t, dsn)
	if err := limA.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	limB := openSQLLimiter(t, dsn)

	const limit = 20
	const perInstance = 20 // 40 total attempts, only 20 may pass

	var (
		allowed int32
		wg      sync.WaitGroup
		mu      sync.Mutex
	)
	run := func(l *SQLLimiter) {
		defer wg.Done()
		for i := 0; i < perInstance; i++ {
			res, err := l.Allow(context.Background(), "shared", limit, time.Minute)
			if err != nil {
				t.Errorf("Allow: %v", err)
				return
			}
			if res.Allowed {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}
	}
	wg.Add(2)
	go run(limA)
	go run(limB)
	wg.Wait()

	if allowed != limit {
		t.Fatalf("allowed across replicas = %d, want exactly %d (shared limit)", allowed, limit)
	}
}
