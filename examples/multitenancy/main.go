// Example: multitenancy demonstrates storage-level tenant isolation using
// storage.WithTenant. Two tenants ("acme" and "globex") share one logical agent
// and one SQLite store, yet neither can see the other's sessions — even when a
// caller tries to read another tenant's session by its exact id (the IDOR the
// control plane closes in production).
//
// The key API is storage.WithTenant(ctx, tenantID): it stamps the tenant on
// every write and scopes every read. Callers that never set a tenant operate
// under storage.DefaultTenant, so single-tenant code keeps working unchanged.
//
// In ChronosOS the tenant is derived from the authenticated principal
// (auth.UserClaims.TenantID) — never from a client-supplied id — so this
// isolation is enforced server-side. This example shows the storage primitive
// that guarantee is built on.
//
// Runs fully offline (SQLite, no API keys):
//
//	go run ./examples/multitenancy/
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

// agentID is the single logical agent shared by every tenant.
const agentID = "support-bot"

func main() {
	ctx := context.Background()

	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║   Chronos Storage-Level Multi-Tenancy Example          ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	dir, err := os.MkdirTemp("", "chronos-mt-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := sqlite.New(filepath.Join(dir, "chronos.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	// Derive a per-tenant context. In production the control plane builds this
	// from the authenticated principal; here we set it explicitly.
	acme := storage.WithTenant(ctx, "acme")
	globex := storage.WithTenant(ctx, "globex")

	// ── 1. Each tenant creates a session on the same agent + same store ──
	acmeSession := newSession("acme-sess-1")
	globexSession := newSession("globex-sess-1")
	must(store.CreateSession(acme, acmeSession))
	must(store.CreateSession(globex, globexSession))

	// ── 2. ListSessions is scoped to the calling tenant ──
	fmt.Println("\n━━━ ListSessions per tenant (same agent, same store) ━━━")
	printSessions(store, acme, "acme")
	printSessions(store, globex, "globex")

	// ── 3. Cross-tenant read by exact id is denied (the IDOR) ──
	fmt.Println("\n━━━ Cross-tenant id lookup (the IDOR the server closes) ━━━")
	// acme knows globex's session id but must not be able to read it.
	if _, err := store.GetSession(acme, "globex-sess-1"); err != nil {
		fmt.Printf("  acme GetSession(\"globex-sess-1\") -> not found (expected): %v\n", err)
	} else {
		log.Fatal("isolation breach: acme read globex's session by id")
	}
	// The owning tenant reads its own session fine.
	if s, err := store.GetSession(globex, "globex-sess-1"); err == nil {
		fmt.Printf("  globex GetSession(\"globex-sess-1\") -> ok: %s (%s)\n", s.ID, s.TenantID)
	} else {
		log.Fatalf("globex could not read its own session: %v", err)
	}

	// ── 4. No tenant set -> DefaultTenant, isolated from both ──
	fmt.Println("\n━━━ Default (single-tenant) callers are isolated too ━━━")
	printSessions(store, ctx, "default")

	fmt.Println("\n✓ Storage-level tenant isolation verified.")
}

func newSession(id string) *storage.Session {
	now := time.Now()
	return &storage.Session{
		ID:        id,
		AgentID:   agentID,
		Status:    "running",
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func printSessions(store storage.Storage, ctx context.Context, label string) {
	sessions, err := store.ListSessions(ctx, agentID, 100, 0)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  %s sees %d session(s):\n", label, len(sessions))
	for _, s := range sessions {
		fmt.Printf("    - %s (tenant=%s)\n", s.ID, s.TenantID)
	}
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
