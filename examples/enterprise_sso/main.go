// Enterprise SSO example for Chronos.
//
// Shows how to run the ChronosOS control plane behind an OIDC identity
// provider (Okta, Auth0, Azure AD, Google Workspace, Keycloak, …). Chronos
// verifies bearer tokens via JWKS — no shared secrets, keys rotate on the
// IdP's schedule, and enforcement lives in one middleware.
//
// The provider is chosen by env: any OIDC issuer with a discoverable JWKS
// URL works. The examples below have been tested with Okta and Auth0 shape
// tokens; Azure AD works identically with issuer set to the tenant URL.
//
//	# Okta
//	export OIDC_ISSUER=https://<tenant>.okta.com/oauth2/default
//	export OIDC_JWKS_URL=https://<tenant>.okta.com/oauth2/default/v1/keys
//	export OIDC_AUDIENCE=api://chronos
//
//	# Azure AD
//	export OIDC_ISSUER=https://login.microsoftonline.com/<tenant-id>/v2.0
//	export OIDC_JWKS_URL=https://login.microsoftonline.com/<tenant-id>/discovery/v2.0/keys
//	export OIDC_AUDIENCE=<application-id>
//
//	# Google Workspace
//	export OIDC_ISSUER=https://accounts.google.com
//	export OIDC_JWKS_URL=https://www.googleapis.com/oauth2/v3/certs
//	export OIDC_AUDIENCE=<oauth-client-id>
//
// Run:
//
//	go run ./examples/enterprise_sso/main.go
//	curl -H "Authorization: Bearer <id-token>" http://localhost:8420/v1/sessions
package main

import (
	"context"
	"log"
	"os"

	chronosos "github.com/spawn08/chronos/os"
	"github.com/spawn08/chronos/os/auth"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

func main() {
	ctx := context.Background()

	store, err := sqlite.New("chronos-sso.db")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	jwtCfg := auth.JWTConfig{
		Issuer:   requireEnv("OIDC_ISSUER"),
		Audience: requireEnv("OIDC_AUDIENCE"),
		JWKSURL:  requireEnv("OIDC_JWKS_URL"),
		// Health probes bypass auth so k8s can hit /healthz.
		SkipPaths: []string{"/healthz", "/livez", "/readyz"},
	}

	srv := chronosos.NewWithOptions(":8420", store,
		chronosos.WithJWTAuth(jwtCfg),
	)

	log.Println("ChronosOS listening on :8420 — bearer tokens verified against", jwtCfg.JWKSURL)
	if err := srv.Start(ctx); err != nil {
		log.Fatal(err)
	}
}

func requireEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("environment variable %s is required", k)
	}
	return v
}
