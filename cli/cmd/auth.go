package cmd

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spawn08/chronos/os/auth"
)

// runAuthCmd implements `chronos auth token`, the missing piece that made
// ChronosOS hard to try locally: there was no CLI command anywhere that
// minted a credential, so the only way to get one was to hand-pick an
// arbitrary API key string (apikey mode) or hand-sign a JWT with an external
// tool (jwt mode). This mints a credential matching whatever CHRONOS_AUTH
// mode the server is (or will be) configured with.
func runAuthCmd() error {
	args := os.Args[2:]
	if len(args) == 0 || args[0] != "token" {
		return fmt.Errorf("usage: chronos auth token [--role <role>] [--tenant <id>] [--ttl <duration>]\n" +
			"Mints a credential matching the active CHRONOS_AUTH mode (apikey or jwt) for local use.")
	}

	role := "admin"
	tenant := ""
	ttl := 24 * time.Hour
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--role":
			if i+1 >= len(rest) {
				return fmt.Errorf("--role requires a value")
			}
			role = rest[i+1]
			i++
		case "--tenant":
			if i+1 >= len(rest) {
				return fmt.Errorf("--tenant requires a value")
			}
			tenant = rest[i+1]
			i++
		case "--ttl":
			if i+1 >= len(rest) {
				return fmt.Errorf("--ttl requires a value")
			}
			d, err := time.ParseDuration(rest[i+1])
			if err != nil {
				return fmt.Errorf("--ttl: %w", err)
			}
			ttl = d
			i++
		default:
			return fmt.Errorf("unknown flag %q (want --role, --tenant, or --ttl)", rest[i])
		}
	}

	mode := strings.ToLower(strings.TrimSpace(os.Getenv(envAuthMode)))
	switch mode {
	case "", "none":
		return fmt.Errorf("%s is unset (or \"none\") — the server has no auth configured, so no credential is needed; set %s=apikey or %s=jwt first", envAuthMode, envAuthMode, envAuthMode)

	case "apikey":
		key, err := generateAPIKey()
		if err != nil {
			return fmt.Errorf("generate api key: %w", err)
		}
		entry := key + ":" + role
		if tenant != "" {
			entry += ":" + tenant
		}
		fmt.Println("Generated API key — add it to CHRONOS_API_KEYS before starting the server:")
		fmt.Println()
		fmt.Printf("  %s=%s\n", envAPIKeys, entry)
		fmt.Println()
		fmt.Println("Then authenticate requests with:")
		fmt.Printf("  curl -H 'X-Api-Key: %s' http://localhost:8420/api/sessions\n", key)
		return nil

	case "jwt":
		secret := strings.TrimSpace(os.Getenv(envJWTSecret))
		if secret == "" {
			return fmt.Errorf("%s=jwt requires %s to be set before a token can be signed", envAuthMode, envJWTSecret)
		}
		claims := auth.UserClaims{
			UserID:   "cli-issued",
			Roles:    []string{role},
			TenantID: tenant,
			Issuer:   strings.TrimSpace(os.Getenv(envJWTIssuer)),
			Exp:      time.Now().Add(ttl).Unix(),
		}
		if aud := strings.TrimSpace(os.Getenv(envJWTAudience)); aud != "" {
			claims.Audience = auth.Audience{aud}
		}
		token := auth.CreateTestToken(claims, secret)
		fmt.Printf("Generated JWT (valid for %s):\n", ttl)
		fmt.Println()
		fmt.Println(" ", token)
		fmt.Println()
		fmt.Println("Then authenticate requests with:")
		fmt.Printf("  curl -H 'Authorization: Bearer %s' http://localhost:8420/api/sessions\n", token)
		return nil

	default:
		return fmt.Errorf("unknown %s=%q (want none, jwt, or apikey)", envAuthMode, mode)
	}
}

// generateAPIKey returns a random, URL-safe key with no ':' or ',' so it is
// always a valid CHRONOS_API_KEYS entry (parseAPIKeys uses ':' and ',' as
// field/entry delimiters).
func generateAPIKey() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return "sk_" + base64.RawURLEncoding.EncodeToString(b), nil
}
