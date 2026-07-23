// Example: server_embedded — running the ChronosOS control plane as a library.
//
// What you'll learn:
//   - How to construct a chronosos.Server with chronosos.NewWithOptions
//   - How to enable API-key authentication, RBAC, and the Swagger UI
//   - How to start the server and shut it down gracefully on SIGINT/SIGTERM
//
// ChronosOS is the HTTP control plane (sessions, traces, events, approvals,
// metrics). Instead of running the `chronos serve` CLI you can embed the same
// server inside your own Go binary, wiring it to your storage backend.
//
// This example compiles with no credentials. The Handler() it builds is what
// the accompanying test exercises in-process with net/http/httptest, so CI
// never binds a port.
//
// Run:
//
//	go run ./examples/server_embedded/          # serves on :8420 until Ctrl-C
//	curl http://localhost:8420/health/ready
//	curl -H "X-Api-Key: dev-secret-key" http://localhost:8420/api/sessions
//	open  http://localhost:8420/swagger/
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	chronosos "github.com/spawn08/chronos/os"
	"github.com/spawn08/chronos/os/auth"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

func main() {
	// Graceful shutdown: cancel the context on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 1. Storage backing the control plane (sessions, traces, events, …).
	store, err := sqlite.New("chronos-embedded.db")
	if err != nil {
		log.Fatalf("open storage: %v", err)
	}
	defer store.Close()

	// 2. Construct the server with options.
	srv := newServer(":8420", store)

	// 3. Start blocks until the context is canceled, then shuts down gracefully.
	log.Println("ChronosOS embedded — listening on :8420 (Ctrl-C to stop)")
	if err := srv.Start(ctx); err != nil {
		log.Fatalf("server: %v", err)
	}
	log.Println("ChronosOS stopped cleanly.")
}

// newServer builds the configured control-plane server. It is a standalone
// function (not inlined into main) so the test can build the exact same server
// and exercise its Handler() in-process.
func newServer(addr string, store *sqlite.Store) *chronosos.Server {
	return chronosos.NewWithOptions(addr, store,
		// Require an API key on /api/* routes. Health, metrics, and Swagger
		// stay reachable without one.
		chronosos.WithAPIKeyAuth(auth.APIKeyConfig{
			Keys: map[string]auth.APIKeyEntry{
				"dev-secret-key": {Scope: "user", UserID: "dev", TenantID: "local"},
			},
		}),
		// Enforce method-based roles once authenticated (viewer to read).
		chronosos.WithRBAC(true),
		// Serve the interactive OpenAPI docs at /swagger/.
		chronosos.WithSwagger(true),
	)
}
