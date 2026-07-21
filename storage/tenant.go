package storage

import "context"

// DefaultTenant is the tenant id used when a context carries no explicit tenant.
// It preserves single-tenant behavior for callers that do not opt into
// multi-tenancy: all such reads and writes are scoped to this tenant.
const DefaultTenant = "default"

// tenantContextKey is the private context key under which the tenant id is
// stored. Using an unexported struct type avoids collisions with keys set by
// other packages.
type tenantContextKey struct{}

// WithTenant returns a copy of ctx that carries the given tenant id. Storage
// adapters scope every read and write to this tenant, making cross-tenant
// access impossible. An empty id is normalized to DefaultTenant.
//
// The control plane derives the tenant from the authenticated principal (JWT or
// API key) and never from client-supplied object ids; see the ChronosOS server.
func WithTenant(ctx context.Context, tenantID string) context.Context {
	if tenantID == "" {
		tenantID = DefaultTenant
	}
	return context.WithValue(ctx, tenantContextKey{}, tenantID)
}

// TenantFromContext returns the tenant id carried by ctx, or DefaultTenant when
// no tenant has been set. It never returns an empty string, so adapters can use
// the result directly as a query parameter.
func TenantFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(tenantContextKey{}).(string); ok && v != "" {
		return v
	}
	return DefaultTenant
}
