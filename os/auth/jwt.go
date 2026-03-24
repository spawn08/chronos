package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type contextKey string

const userContextKey contextKey = "user"

// UserClaims represents the decoded user information from a JWT.
type UserClaims struct {
	UserID string   `json:"user_id"`
	Roles  []string `json:"roles"`
	Exp    int64    `json:"exp"`
}

// JWTConfig holds configuration for JWT authentication.
type JWTConfig struct {
	Secret         string
	Issuer         string
	SkipPaths      []string
	AllowExpiredAt time.Duration
}

// UserFromContext extracts UserClaims from the request context.
func UserFromContext(ctx context.Context) (*UserClaims, bool) {
	u, ok := ctx.Value(userContextKey).(*UserClaims)
	return u, ok
}

// WithUser adds UserClaims to the context.
func WithUser(ctx context.Context, claims *UserClaims) context.Context {
	return context.WithValue(ctx, userContextKey, claims)
}

// JWTMiddleware returns HTTP middleware that validates JWT bearer tokens.
func JWTMiddleware(cfg JWTConfig) func(http.Handler) http.Handler {
	skipSet := make(map[string]bool, len(cfg.SkipPaths))
	for _, p := range cfg.SkipPaths {
		skipSet[p] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skipSet[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				http.Error(w, `{"error":"missing or invalid Authorization header"}`, http.StatusUnauthorized)
				return
			}

			token := strings.TrimPrefix(authHeader, "Bearer ")
			claims, err := validateJWT(token, cfg.Secret)
			if err != nil {
				http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusUnauthorized)
				return
			}

			if claims.Exp > 0 && time.Now().Unix() > claims.Exp {
				http.Error(w, `{"error":"token expired"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), userContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func validateJWT(token, secret string) (*UserClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid payload encoding: %w", err)
	}

	signatureInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signatureInput))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expectedSig), []byte(parts[2])) {
		return nil, fmt.Errorf("invalid signature")
	}

	var claims UserClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("invalid claims: %w", err)
	}

	return &claims, nil
}

// CreateTestToken creates a simple HMAC-SHA256 signed JWT for testing.
func CreateTestToken(claims UserClaims, secret string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	payloadEnc := base64.RawURLEncoding.EncodeToString(payload)

	signatureInput := header + "." + payloadEnc
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signatureInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return header + "." + payloadEnc + "." + sig
}
