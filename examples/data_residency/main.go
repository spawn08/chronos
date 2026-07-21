// Data residency example for Chronos.
//
// A single logical agent that keeps EU tenant data on an EU-resident
// PostgreSQL and US tenant data on a US-resident PostgreSQL — routed at
// call time from a tenant claim. The same pattern applies to any pair of
// storage backends (US-Postgres + EU-Postgres, RDS + Cloud SQL,
// on-prem + hyperscaler, or SQLite files partitioned per region).
//
// For a fully offline demo the example uses two SQLite files
// ("residency-eu.db", "residency-us.db"). The routing pattern is
// identical for production Postgres/DynamoDB backends — swap the
// factory. See ROUTING below.
//
// Run:
//
//	go run ./examples/data_residency/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/spawn08/chronos/sdk/memory"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

const agentID = "residency-agent"

// Tenant carries the residency decision. In production this comes from
// the OIDC token's `region` / `tenant` claim, not the app config.
type Tenant struct {
	ID     string
	Region string // "eu" or "us"
}

func main() {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "chronos-residency-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// ── ROUTING ────────────────────────────────────────────────
	// One Storage per region. In prod: sqlite.New → postgres.New
	// with a DSN whose host is region-pinned (eu-west-1 vs us-east-1).
	regions, err := openRegions(ctx, dir)
	if err != nil {
		log.Fatal(err)
	}
	defer closeAll(regions)

	// ── SIMULATED TRAFFIC ──────────────────────────────────────
	tenants := []Tenant{
		{ID: "acme-berlin", Region: "eu"},
		{ID: "globex-nyc", Region: "us"},
		{ID: "initech-paris", Region: "eu"},
	}

	for _, t := range tenants {
		mem, err := memoryFor(t, regions)
		if err != nil {
			log.Fatalf("resolve memory for %s: %v", t.ID, err)
		}
		if err := mem.SetLongTerm(ctx, "onboarded_at", "2026-07-21"); err != nil {
			log.Fatal(err)
		}
		if err := mem.SetLongTerm(ctx, "preferred_channel", "email"); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("wrote tenant=%s → region=%s backend=%s\n",
			t.ID, t.Region, backendPath(regions, t.Region))
	}

	// ── VERIFY ISOLATION ───────────────────────────────────────
	// EU-resident tenants must not be visible in the US store, and
	// vice versa. A compliance audit reads only the store for its
	// jurisdiction.
	for region, store := range regions {
		fmt.Printf("\n--- audit region=%s ---\n", region)
		auditRegion(ctx, store, region)
	}
}

func openRegions(ctx context.Context, dir string) (map[string]storage.Storage, error) {
	out := make(map[string]storage.Storage, 2)
	for _, r := range []string{"eu", "us"} {
		s, err := sqlite.New(filepath.Join(dir, "residency-"+r+".db"))
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", r, err)
		}
		if err := s.Migrate(ctx); err != nil {
			return nil, fmt.Errorf("migrate %s: %w", r, err)
		}
		out[r] = s
	}
	return out, nil
}

func closeAll(regions map[string]storage.Storage) {
	for _, s := range regions {
		_ = s.Close()
	}
}

// memoryFor returns a memory.Store bound to the tenant's residency region.
// The Store id is namespaced by tenant id, so a single logical agent serves
// every tenant while state stays inside the correct backend.
func memoryFor(t Tenant, regions map[string]storage.Storage) (*memory.Store, error) {
	s, ok := regions[t.Region]
	if !ok {
		return nil, fmt.Errorf("no backend for region %q", t.Region)
	}
	return memory.NewStore(agentID+"::"+t.ID, s), nil
}

func backendPath(regions map[string]storage.Storage, region string) string {
	if _, ok := regions[region]; ok {
		return "residency-" + region + ".db"
	}
	return "<none>"
}

func auditRegion(ctx context.Context, s storage.Storage, region string) {
	// The audit query is naive on purpose: list the memories the residency
	// store holds. In prod, replace with your compliance query — the point
	// is that only rows belonging to this region ever exist here.
	tenants := map[string][]string{
		"eu": {"acme-berlin", "initech-paris", "globex-nyc"},
		"us": {"globex-nyc", "acme-berlin", "initech-paris"},
	}[region]
	for _, tid := range tenants {
		mem := memory.NewStore(agentID+"::"+tid, s)
		items, err := mem.ListLongTerm(ctx)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  tenant=%-15s memories=%d\n", tid, len(items))
	}
}
