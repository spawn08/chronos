---
title: "Authentication & Authorization"
sidebar_label: "Authentication"
---


The [ChronosOS server](/guides/server) ships with authentication and
role-based access control (RBAC) that are **opt-in**. By default the server runs
with **no authentication** — every request is allowed and runs under the single
`DefaultTenant`. This is convenient for local development and trusted networks;
for anything internet-facing, enable one of the auth modes below.

Two modes are supported, selected with the `CHRONOS_AUTH` environment variable:

| Mode | `CHRONOS_AUTH` | Credential |
|------|----------------|------------|
| None (default) | `none` (or unset) | — |
| JWT bearer tokens | `jwt` | `Authorization: Bearer <token>` |
| API keys | `apikey` | `X-Api-Key: <key>` |

:::note Always-public endpoints
Health (`/healthz`, `/health`, `/health/live`, `/health/ready`), metrics
(`/metrics`), and Swagger (`/swagger*`) **bypass auth in every mode**, so probes
and scrapers keep working without credentials.
:::

## Environment variables

| Variable | Mode | Default | Description |
|----------|------|---------|-------------|
| `CHRONOS_AUTH` | all | `none` | `none` \| `jwt` \| `apikey` |
| `CHRONOS_RBAC` | all | `false` | Opt-in role enforcement on `/api/*` (only effective when auth is enabled) |
| `CHRONOS_SWAGGER` | all | `true` | Set `false` to disable the Swagger UI and OpenAPI spec |
| `CHRONOS_JWT_SECRET` | jwt | — | HS256 shared secret |
| `CHRONOS_JWT_ISSUER` | jwt | — | Expected `iss` claim |
| `CHRONOS_JWT_AUDIENCE` | jwt | — | Expected `aud` claim |
| `CHRONOS_JWT_JWKS_URL` | jwt | — | JWKS endpoint for RS256/OIDC (keys cached by `kid`, rotation-aware) |
| `CHRONOS_API_KEYS` | apikey | — | Comma-separated `key:role:tenant` entries (see [API-key authentication](#api-key-authentication)) |

## JWT authentication

Set `CHRONOS_AUTH=jwt` and provide validation material. Callers send a bearer
token in the `Authorization` header:

```
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

### HS256 (shared secret)

Simplest setup — a single symmetric secret shared between issuer and Chronos:

```bash
export CHRONOS_AUTH=jwt
export CHRONOS_JWT_SECRET="a-long-random-secret"
export CHRONOS_JWT_ISSUER="https://auth.acme.com"
export CHRONOS_JWT_AUDIENCE="chronos"
chronos serve :8420
```

### RS256 / JWKS / OIDC

For production SSO (Okta, Azure AD, Google, Auth0, Keycloak), point Chronos at
the issuer's JWKS URL. Public keys are fetched and **cached by `kid`**, and the
cache refreshes automatically on **key rotation** — so tokens keep validating
across issuer key rollovers:

```bash
export CHRONOS_AUTH=jwt
export CHRONOS_JWT_ISSUER="https://acme.okta.com"
export CHRONOS_JWT_AUDIENCE="api://chronos"
export CHRONOS_JWT_JWKS_URL="https://acme.okta.com/oauth2/v1/keys"
chronos serve :8420
```

A static RS256 public key is also supported instead of JWKS, and **HS256 and
RS256 can co-exist** — the algorithm is chosen per-token from its header.

:::warning Token expiry
Tokens must carry an `exp` claim unless their issuer is explicitly trusted.
Prefer **short-lived** access tokens (minutes, not days) and refresh them out of
band.
:::

### SDK equivalent

The SDK snippets in this section use these imports:

```go
import (
    chronosos "github.com/spawn08/chronos/os"
    "github.com/spawn08/chronos/os/auth"
)
```

```go
srv := chronosos.NewWithOptions(":8420", store,
    chronosos.WithJWTAuth(auth.JWTConfig{
        Issuer:   "https://acme.okta.com",
        Audience: "api://chronos",
        JWKSURL:  "https://acme.okta.com/oauth2/v1/keys",
    }),
)
```

### Authenticated request

```bash
TOKEN="eyJhbGciOiJIUzI1NiI..."
curl "http://localhost:8420/api/sessions" \
  -H "Authorization: Bearer $TOKEN"
```

## API-key authentication

Set `CHRONOS_AUTH=apikey` and declare keys as `key:role:tenant` triples. Callers
send the key in the `X-Api-Key` header (the header name is configurable via the
SDK):

```bash
export CHRONOS_AUTH=apikey
export CHRONOS_API_KEYS="k_live_admin:admin:acme,k_live_ro:viewer:acme,k_beta:user:globex"
chronos serve :8420
```

Each entry is `key:role:tenant`, entries separated by commas. Only `key` is
required; `role` and `tenant` are the optional 2nd and 3rd colon-separated
fields.

:::warning Key format
The **key value itself must not contain `:` or `,`** — those are the field and
entry delimiters. Use URL-safe/opaque tokens (e.g. `k_live_…`) so parsing stays
unambiguous.
:::

Keys are stored **hashed** and compared in **constant time** to resist timing
attacks — the plaintext key is never held or logged after startup.

```bash
curl "http://localhost:8420/api/sessions" \
  -H "X-Api-Key: k_live_ro"
```

### Quotas

API keys support optional **per-key and per-tenant quotas**. When a key or tenant
exceeds its quota, the server returns `429 Too Many Requests`. This is independent
of the global [rate limiter](/guides/server#middleware-stack) and lets you cap
individual clients.

### SDK equivalent

```go
srv := chronosos.NewWithOptions(":8420", store,
    chronosos.WithAPIKeyAuth(auth.APIKeyConfig{
        HeaderName: "X-Api-Key",
        // Map of raw key -> entry. Keys are hashed at construction; the
        // plaintext is not retained. Scope carries the RBAC role.
        Keys: map[string]auth.APIKeyEntry{
            "k_live_admin": {Scope: "admin", TenantID: "acme"},
            "k_live_ro":    {Scope: "viewer", TenantID: "acme"},
        },
    }),
)
```

## Roles & RBAC

Every principal (JWT claim or API key) carries a **role**. Roles form a strict
hierarchy — a higher role includes every permission of the roles below it:

```
admin (3)  >  user (2)  >  viewer (1)
```

| Role | Can do |
|------|--------|
| `viewer` | Read-only: list/get sessions, state, traces, schedules, approvals, subscribe to SSE |
| `user` | Everything `viewer` can, plus mutate: patch state, create/delete schedules, respond to approvals |
| `admin` | Everything `user` can, plus administrative operations |

The role comes from the authenticated principal's claims — the JWT `roles` claim,
or the role field of the matching `CHRONOS_API_KEYS` entry.

### Enabling enforcement (`CHRONOS_RBAC`)

Role enforcement on `/api/*` is **opt-in** and off by default. It is only
effective when authentication is also enabled (`CHRONOS_AUTH=jwt|apikey`) — with
`CHRONOS_AUTH=none` there is no principal to derive a role from. Turn it on with:

```bash
export CHRONOS_AUTH=jwt
export CHRONOS_RBAC=true
chronos serve :8420
```

When enabled, the server gates every `/api/*` route by HTTP method:

| Request | Required role |
|---------|---------------|
| Read (`GET` / `HEAD`) | `viewer` |
| Mutating (`POST` / `PUT` / `PATCH` / `DELETE`) | `user` |

Because roles are hierarchical, an `admin` or `user` token also satisfies
`viewer`-level reads. A request with **no** valid credential receives
`401 Unauthorized`; an authenticated but **under-privileged** request receives
`403 Forbidden`. The per-endpoint requirement is listed in the
[REST API Reference](/api/rest-api).

SDK equivalent:

```go
srv := chronosos.NewWithOptions(":8420", store,
    chronosos.WithJWTAuth(auth.JWTConfig{ /* … */ }),
    chronosos.WithRBAC(true),
)
```

## Tenant isolation

Each principal's claims include a `TenantID`. The server derives the tenant from
the authenticated principal and scopes **every storage operation** to it. A
caller therefore sees only their own tenant's sessions, checkpoints, traces,
schedules, and approvals — requesting another tenant's `session_id` yields `404`
rather than leaking data (**IDOR-safe**).

With auth disabled, all traffic runs under `DefaultTenant`. See
[Multi-Tenancy](/guides/multi-tenancy) for the storage model.

## Security best practices

- **Terminate TLS at the ingress.** Chronos speaks plain HTTP; run it behind an
  ingress/load balancer that terminates TLS. Never send bearer tokens or API keys
  over cleartext.
- **Short token TTLs.** Keep JWT `exp` small and refresh out of band. Require
  `exp` on all tokens.
- **Rotate keys and secrets.** Rotate API keys and the HS256 secret regularly;
  JWKS rotation is handled automatically for RS256 issuers.
- **Least privilege.** Hand out `viewer` by default; grant `user`/`admin` only
  where mutation is required.
- **Store secrets externally.** Inject `CHRONOS_*` secrets from a secret manager
  (Vault, AWS Secrets Manager, Kubernetes Secrets) — never commit them.
- **Keep rate limiting on** and use `WithRateLimiter(middleware.NewSQLLimiter(db, dialect))` across replicas so
  limits and quotas are enforced fleet-wide.
- **Disable Swagger on hardened control planes.** The Swagger UI and OpenAPI spec
  intentionally **bypass auth** (so docs stay reachable), which means the schema
  and interactive console are reachable **anonymously** when enabled. On a
  locked-down production server set `CHRONOS_SWAGGER=false` (SDK:
  `chronosos.WithSwagger(false)`) to remove `/swagger` and `/swagger/doc.json`
  entirely.

For a full production wiring (JWKS auth + TLS ingress + Postgres), see the
[`deploy/production/`](https://github.com/spawn08/chronos/tree/main/deploy/production)
example and the [enterprise SSO example](https://github.com/spawn08/chronos/tree/main/examples/enterprise_sso).

## See also

- [ChronosOS Server](/guides/server) — the server this secures
- [REST API Reference](/api/rest-api) — per-endpoint auth requirements
- [Multi-Tenancy](/guides/multi-tenancy) — tenant isolation at the storage layer
