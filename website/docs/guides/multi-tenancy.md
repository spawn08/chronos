---
title: "Multi-Tenancy"
---

Chronos supports **hard tenant isolation** at the storage layer. Multiple tenants
(organizations, workspaces, end-customers) can share one Chronos deployment — one
agent, one database — while each tenant's sessions, memory, traces, audit logs,
events, and checkpoints stay completely invisible to every other tenant.

Isolation is **opt-in and backward-compatible**: code that never sets a tenant
runs under a single `DefaultTenant`, so existing single-tenant applications keep
working unchanged.

## The core API

Tenant identity travels on the `context.Context` that you already pass to every
storage call:

```go
import (
    "context"

    "github.com/spawn08/chronos/storage"
)

// Stamp the tenant on the context.
ctx := storage.WithTenant(context.Background(), "acme")

// Every read and write on this ctx is now scoped to tenant "acme".
// store implements storage.Storage; session is a *storage.Session; agentID is a string.
store.CreateSession(ctx, session)                       // written with tenant_id = "acme"
sessions, _ := store.ListSessions(ctx, agentID, 100, 0) // returns only "acme" sessions
```

Two helpers make up the whole surface:

| Function | Behavior |
|----------|----------|
| `storage.WithTenant(ctx, id)` | Returns a child context carrying the tenant id. An empty id normalizes to `DefaultTenant`. |
| `storage.TenantFromContext(ctx)` | Returns the tenant id on the context, or `DefaultTenant` if none was set. Never returns `""`. |

The constant `storage.DefaultTenant` (`"default"`) is the fallback tenant for any
call that does not opt in.

## What isolation guarantees

Given a store shared by tenants `acme` and `globex`:

- **Scoped writes** — every record is stamped with the calling context's tenant.
  The `tenant_id` is taken from the context, not from the struct, so a client
  cannot spoof it by setting a field.
- **Scoped list/queries** — `ListSessions`, `ListTraces`, `ListEvents`,
  `ListCheckpoints`, memory reads, etc. return only the calling tenant's rows.
- **Scoped id lookups** — `GetSession`, `GetTrace`, and `GetCheckpoint` return a
  not-found error when the object belongs to another tenant, even if the caller
  supplies the exact id. This closes the IDOR (insecure direct object reference)
  where knowing an id was enough to read someone else's data.

```go
acme   := storage.WithTenant(ctx, "acme")
globex := storage.WithTenant(ctx, "globex")

store.CreateSession(globex, &storage.Session{ID: "sess-1", AgentID: "bot"})

// acme knows the id but cannot read it:
_, err := store.GetSession(acme, "sess-1") // -> not found

// the owning tenant reads it fine:
s, _ := store.GetSession(globex, "sess-1") // -> ok, s.TenantID == "globex"
```

A complete runnable demo lives in
[`examples/multitenancy`](https://github.com/spawn08/chronos/tree/main/examples/multitenancy)
(offline, SQLite, no API keys).

## Enforcement in ChronosOS

In the control plane you must never trust a client-supplied tenant id. ChronosOS
derives the tenant from the **authenticated principal** — the `TenantID` claim on
the JWT or API key (`auth.UserClaims.TenantID`) — and builds the tenant context
from that before touching storage:

```go
// Inside a ChronosOS handler (simplified):
tenantCtx := storage.WithTenant(r.Context(), claims.TenantID)
sessions, _ := srv.store.ListSessions(tenantCtx, agentID, limit, offset)
```

As a result, a request authenticated as tenant A that asks for a session, trace,
or checkpoint owned by tenant B receives an empty result or a `404` — the
cross-tenant read is impossible even with a valid, guessed object id.

## Storage support

| Backend | Tenant isolation |
|---------|------------------|
| SQLite | ✅ Enforced — `tenant_id` column + composite indexes |
| PostgreSQL | ✅ Enforced — `tenant_id` column + composite indexes |
| In-memory | ✅ Enforced |
| DynamoDB / MongoDB / Redis / RedisVector | ⚠️ Experimental — data currently lands under `DefaultTenant`; per-tenant scoping is not yet enforced on these backends |

The relational adapters add the `tenant_id` column via **migration v2** (applied
automatically by `Migrate`) and index every table on `(tenant_id, …)`, so scoped
queries stay fast at scale.

## Migrating an existing single-tenant database

Running `Migrate` on an existing SQLite/Postgres database adds the `tenant_id`
column with a default of `"default"`, so all pre-existing rows become owned by
`DefaultTenant`. Existing code that doesn't call `WithTenant` continues to read
and write exactly those rows — no data is lost and no code change is required.
Adopt tenancy incrementally by wrapping contexts with `WithTenant` where you have
a tenant identity available.

## Related

- [Storage Adapters](./storage.md) — the `Storage` interface and adapter matrix.
- [Data residency example](https://github.com/spawn08/chronos/tree/main/examples/data_residency) — routing tenants to region-resident databases.
