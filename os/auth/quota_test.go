package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

func TestMemoryQuotaStore_RequestsPerMinute(t *testing.T) {
	store := NewMemoryQuotaStore()
	q := Quota{RequestsPerMinute: 3}
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		ok, err := store.Allow(ctx, "key:x", q)
		if err != nil || !ok {
			t.Fatalf("request %d: ok=%v err=%v", i, ok, err)
		}
	}
	ok, err := store.Allow(ctx, "key:x", q)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ok {
		t.Error("4th request should be denied")
	}

	// A different subject is unaffected.
	if ok, _ := store.Allow(ctx, "key:y", q); !ok {
		t.Error("separate subject should be allowed")
	}
}

func TestMemoryQuotaStore_TokenAndCostBudget(t *testing.T) {
	store := NewMemoryQuotaStore()
	ctx := context.Background()

	tokQuota := Quota{TokensPerDay: 100}
	if ok, _ := store.Allow(ctx, "k", tokQuota); !ok {
		t.Fatal("first request within token budget should pass")
	}
	if err := store.AddUsage(ctx, "k", 150, 0); err != nil {
		t.Fatalf("add usage: %v", err)
	}
	if ok, _ := store.Allow(ctx, "k", tokQuota); ok {
		t.Error("over token budget should be denied")
	}

	costQuota := Quota{MaxCostUSD: 1.0}
	if err := store.AddUsage(ctx, "c", 0, 2.5); err != nil {
		t.Fatalf("add usage: %v", err)
	}
	if ok, _ := store.Allow(ctx, "c", costQuota); ok {
		t.Error("over cost budget should be denied")
	}

	usage, err := store.Usage(ctx, "c")
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if usage.CostUSD != 2.5 {
		t.Errorf("got cost %v, want 2.5", usage.CostUSD)
	}
}

func TestMemoryQuotaStore_Concurrent(t *testing.T) {
	store := NewMemoryQuotaStore()
	q := Quota{RequestsPerMinute: 1000}
	var allowed int64
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := store.Allow(context.Background(), "shared", q); ok {
				atomic.AddInt64(&allowed, 1)
			}
			_ = store.AddUsage(context.Background(), "shared", 1, 0.001)
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt64(&allowed); got != 200 {
		t.Errorf("allowed %d, want 200 (all under limit)", got)
	}
}

func TestAPIKeyMiddleware_QuotaEnforced(t *testing.T) {
	quotas := NewMemoryQuotaStore()
	cfg := APIKeyConfig{
		Keys: map[string]APIKeyEntry{
			"limited-key": {Scope: "user", UserID: "u", TenantID: "t1", Quota: Quota{RequestsPerMinute: 2}},
		},
		Quotas: quotas,
	}
	handler := APIKeyMiddleware(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	do := func() int {
		req := httptest.NewRequest("GET", "/api/test", http.NoBody)
		req.Header.Set("X-Api-Key", "limited-key")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := do(); code != http.StatusOK {
		t.Errorf("req 1: got %d, want 200", code)
	}
	if code := do(); code != http.StatusOK {
		t.Errorf("req 2: got %d, want 200", code)
	}
	if code := do(); code != http.StatusTooManyRequests {
		t.Errorf("req 3: got %d, want 429", code)
	}
}

func TestAPIKeyMiddleware_TenantQuotaEnforced(t *testing.T) {
	quotas := NewMemoryQuotaStore()
	cfg := APIKeyConfig{
		Keys: map[string]APIKeyEntry{
			"key1": {Scope: "user", UserID: "u1", TenantID: "acme"},
			"key2": {Scope: "user", UserID: "u2", TenantID: "acme"},
		},
		Quotas:       quotas,
		TenantQuotas: map[string]Quota{"acme": {RequestsPerMinute: 2}},
	}
	handler := APIKeyMiddleware(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	)
	do := func(key string) int {
		req := httptest.NewRequest("GET", "/api/test", http.NoBody)
		req.Header.Set("X-Api-Key", key)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}
	// Two different keys under the same tenant share the tenant limit.
	if code := do("key1"); code != http.StatusOK {
		t.Errorf("req 1: got %d, want 200", code)
	}
	if code := do("key2"); code != http.StatusOK {
		t.Errorf("req 2: got %d, want 200", code)
	}
	if code := do("key1"); code != http.StatusTooManyRequests {
		t.Errorf("req 3: got %d, want 429 (tenant limit)", code)
	}
}
