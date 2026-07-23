package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

// TestEmbeddedServerHandler exercises the exact server built by the example
// in-process via httptest — no port is bound, so it is CI- and network-safe.
func TestEmbeddedServerHandler(t *testing.T) {
	ctx := context.Background()

	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	srv := newServer(":0", store)
	srv.SetReady(true) // Start() would set this; we call Handler() directly.

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	tests := []struct {
		name     string
		method   string
		path     string
		apiKey   string
		wantCode int
	}{
		{name: "readiness is public", method: http.MethodGet, path: "/health/ready", wantCode: http.StatusOK},
		{name: "swagger is public", method: http.MethodGet, path: "/swagger/", wantCode: http.StatusOK},
		{name: "api rejects missing key", method: http.MethodGet, path: "/api/sessions", wantCode: http.StatusUnauthorized},
		{name: "api accepts valid key", method: http.MethodGet, path: "/api/sessions", apiKey: "dev-secret-key", wantCode: http.StatusOK},
	}

	client := ts.Client()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(ctx, tt.method, ts.URL+tt.path, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			if tt.apiKey != "" {
				req.Header.Set("X-Api-Key", tt.apiKey)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("do request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantCode {
				t.Errorf("%s %s = %d, want %d", tt.method, tt.path, resp.StatusCode, tt.wantCode)
			}
		})
	}
}
