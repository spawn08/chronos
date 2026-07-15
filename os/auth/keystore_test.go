package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestMemoryKeyStore_RoundTrip(t *testing.T) {
	store := NewMemoryKeyStore()
	ctx := context.Background()

	rec, err := store.Add(ctx, "raw-secret-key", KeyRecord{ID: "k1", Scope: "admin", UserID: "u1", TenantID: "t1"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if rec.Hash == "" || rec.Hash == "raw-secret-key" {
		t.Fatalf("raw key must be hashed, got %q", rec.Hash)
	}
	if rec.Hash != HashKey("raw-secret-key") {
		t.Errorf("hash mismatch")
	}

	got, ok, err := store.Lookup(ctx, "raw-secret-key")
	if err != nil || !ok {
		t.Fatalf("lookup: ok=%v err=%v", ok, err)
	}
	if got.UserID != "u1" || got.Scope != "admin" || got.TenantID != "t1" {
		t.Errorf("bad record: %+v", got)
	}

	// Wrong key does not match.
	if _, ok, _ := store.Lookup(ctx, "wrong-key"); ok {
		t.Error("wrong key should not match")
	}

	// Raw key never persisted.
	for _, r := range mustList(t, store) {
		if r.Hash == "raw-secret-key" {
			t.Error("raw key leaked into store")
		}
	}

	// Disabled keys are not matched.
	rec.Disabled = true
	if _, err := store.Add(ctx, "raw-secret-key", rec); err != nil {
		t.Fatalf("re-add: %v", err)
	}
	if _, ok, _ := store.Lookup(ctx, "raw-secret-key"); ok {
		t.Error("disabled key should not match")
	}

	// Remove.
	if err := store.Remove(ctx, "k1"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got := mustList(t, store); len(got) != 0 {
		t.Errorf("expected empty store, got %d", len(got))
	}
}

func mustList(t *testing.T, s *MemoryKeyStore) []KeyRecord {
	t.Helper()
	recs, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return recs
}

func TestMemoryKeyStore_Concurrent(t *testing.T) {
	store := NewMemoryKeyStore()
	ctx := context.Background()
	if _, err := store.Add(ctx, "key-a", KeyRecord{ID: "a", UserID: "ua"}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if n%2 == 0 {
				_, _, _ = store.Lookup(ctx, "key-a")
			} else {
				_, _ = store.Add(ctx, "key-b", KeyRecord{ID: "b", UserID: "ub"})
			}
			_, _ = store.List(ctx)
		}(i)
	}
	wg.Wait()

	if _, ok, _ := store.Lookup(ctx, "key-a"); !ok {
		t.Error("key-a should still resolve")
	}
}

func TestAPIKeyMiddleware_HashedStore(t *testing.T) {
	store := NewMemoryKeyStore()
	if _, err := store.Add(context.Background(), "persisted-key", KeyRecord{ID: "pk", Scope: "user", UserID: "pu"}); err != nil {
		t.Fatal(err)
	}
	handler := APIKeyMiddleware(APIKeyConfig{Store: store})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := UserFromContext(r.Context())
			if !ok || u.UserID != "pu" {
				t.Errorf("bad claims: %+v ok=%v", u, ok)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)
	req := httptest.NewRequest("GET", "/api/test", http.NoBody)
	req.Header.Set("X-Api-Key", "persisted-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rec.Code)
	}
}
