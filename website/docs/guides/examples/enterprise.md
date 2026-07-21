---
title: "Enterprise & Multi-Tenancy"
---


# Enterprise & Multi-Tenancy

The things enterprise procurement asks about on day one: single sign-on, data residency, and tenant isolation a compliance team will sign off on.

---

## enterprise_sso

**ChronosOS control plane behind an OIDC identity provider** (Okta, Azure AD, Google Workspace, Auth0, Keycloak). Bearer tokens are verified against the IdP's JWKS endpoint — no shared secrets, keys rotate on the IdP's schedule, enforcement lives in one middleware.

```bash
# Okta
export OIDC_ISSUER=https://<tenant>.okta.com/oauth2/default
export OIDC_JWKS_URL=https://<tenant>.okta.com/oauth2/default/v1/keys
export OIDC_AUDIENCE=api://chronos

# Azure AD
export OIDC_ISSUER=https://login.microsoftonline.com/<tenant-id>/v2.0
export OIDC_JWKS_URL=https://login.microsoftonline.com/<tenant-id>/discovery/v2.0/keys
export OIDC_AUDIENCE=<application-id>

# Google Workspace
export OIDC_ISSUER=https://accounts.google.com
export OIDC_JWKS_URL=https://www.googleapis.com/oauth2/v3/certs
export OIDC_AUDIENCE=<oauth-client-id>

go run ./examples/enterprise_sso/
curl -H "Authorization: Bearer <id-token>" http://localhost:8420/v1/sessions
```

**Demonstrates:**
- `chronosos.NewWithOptions(addr, store, chronosos.WithJWTAuth(...))`
- `auth.JWTConfig{ Issuer, Audience, JWKSURL }` — OIDC/JWKS verification with `kid`-cached rotation
- Health-probe paths (`/healthz`, `/livez`, `/readyz`) bypassing auth for Kubernetes

---

## data_residency

**Per-tenant storage routing** — a single logical agent that keeps EU tenant data on an EU-resident backend and US tenant data on a US-resident backend, chosen at call time from a tenant/region claim. Runs offline with two SQLite files; the routing pattern is identical for region-pinned Postgres or DynamoDB.

```bash
go run ./examples/data_residency/
```

**Demonstrates:**
- One `storage.Storage` per residency region, selected from a `Tenant.Region` value
- `memory.NewStore(agentID + "::" + tenantID, backend)` — tenant-namespaced state on the correct backend
- A compliance-style audit proving EU rows never appear in the US store (and vice versa)

---

## multitenant_memory

**Per-tenant long-term memory isolation on one logical agent.** Two users served by the same agent, whose memories never leak across tenants — the deterministic core needs no API key.

```bash
go run ./examples/multitenant_memory/
```

**Demonstrates:**
- Namespacing a memory `Store` by `agentID + "::" + userID`
- Proving Ada and Bob each see only their own long-term memories
- An optional LLM-powered `memory.Manager` section guarded behind `OPENAI_API_KEY`

Source: [examples/multitenant_memory](https://github.com/spawn08/chronos/tree/main/examples/multitenant_memory)
