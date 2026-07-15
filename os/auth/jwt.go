package auth

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type contextKey string

const userContextKey contextKey = "user"

// Audience represents a JWT "aud" claim which may be encoded either as a single
// string or as an array of strings. It normalizes both forms to a slice.
type Audience []string

// UnmarshalJSON accepts both a JSON string and a JSON array of strings.
func (a *Audience) UnmarshalJSON(b []byte) error {
	var single string
	if err := json.Unmarshal(b, &single); err == nil {
		*a = Audience{single}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return fmt.Errorf("invalid audience claim: %w", err)
	}
	*a = many
	return nil
}

// MarshalJSON emits a single string when there is exactly one audience,
// otherwise a JSON array.
func (a Audience) MarshalJSON() ([]byte, error) {
	if len(a) == 1 {
		return json.Marshal(a[0])
	}
	return json.Marshal([]string(a))
}

// Contains reports whether the audience set includes the given value.
func (a Audience) Contains(v string) bool {
	for _, s := range a {
		if s == v {
			return true
		}
	}
	return false
}

// UserClaims represents the decoded user information from a JWT.
type UserClaims struct {
	UserID   string   `json:"user_id,omitempty"`
	Roles    []string `json:"roles,omitempty"`
	Exp      int64    `json:"exp,omitempty"`
	Issuer   string   `json:"iss,omitempty"`
	Audience Audience `json:"aud,omitempty"`
	Subject  string   `json:"sub,omitempty"`
	TenantID string   `json:"tenant_id,omitempty"`
}

// JWTConfig holds configuration for JWT authentication.
//
// Backward compatibility: an HS256 deployment only needs Secret (and optionally
// Issuer/Audience/SkipPaths). RS256/OIDC deployments set either RSAPublicKey (a
// static key) or JWKSURL (dynamic keys fetched and cached by "kid", honoring
// rotation). The verification path is selected per-token from the JWS "alg"
// header, so both HS256 and RS256 tokens can be accepted by the same server if
// both a Secret and an RSA key source are configured.
type JWTConfig struct {
	// Secret is the shared HMAC secret for HS256 tokens. Required to accept
	// HS256 tokens; leave empty to reject them.
	Secret string
	// Issuer, when set, is enforced against the token "iss" claim.
	Issuer string
	// Audience, when set, is enforced against the token "aud" claim.
	Audience string
	// SkipPaths are request paths that bypass authentication entirely.
	SkipPaths []string

	// AllowExpiredAt is the grace window during which an already-expired token
	// is still accepted, but ONLY on paths listed in AllowExpiredPaths (i.e.
	// explicit token-refresh endpoints). It has no effect elsewhere.
	AllowExpiredAt time.Duration
	// AllowExpiredPaths enumerates the refresh-flow paths on which
	// AllowExpiredAt applies.
	AllowExpiredPaths []string

	// RSAPublicKey is a static RSA public key for verifying RS256/384/512
	// tokens without JWKS. Mutually usable with JWKSURL (JWKS takes precedence
	// when a matching kid is found).
	RSAPublicKey *rsa.PublicKey
	// JWKSURL is an OIDC/JWKS endpoint. Keys are fetched, cached by "kid", and
	// re-fetched on cache miss (rotation) or expiry.
	JWKSURL string
	// JWKSCacheTTL bounds how long JWKS entries are cached before a refresh.
	// Defaults to defaultJWKSCacheTTL when zero.
	JWKSCacheTTL time.Duration
	// HTTPClient is used for JWKS fetches; defaults to a client with a short
	// timeout when nil.
	HTTPClient *http.Client

	// now is injectable for tests; defaults to time.Now.
	now func() time.Time
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

// jwtVerifier resolves keys and verifies token signatures for a JWTConfig.
// It is constructed once per middleware so the JWKS cache is shared across
// requests.
type jwtVerifier struct {
	cfg   JWTConfig
	jwks  *jwksCache
	nowFn func() time.Time
}

func newJWTVerifier(cfg JWTConfig) *jwtVerifier {
	nowFn := cfg.now
	if nowFn == nil {
		nowFn = time.Now
	}
	v := &jwtVerifier{cfg: cfg, nowFn: nowFn}
	if cfg.JWKSURL != "" {
		ttl := cfg.JWKSCacheTTL
		if ttl <= 0 {
			ttl = defaultJWKSCacheTTL
		}
		client := cfg.HTTPClient
		if client == nil {
			client = &http.Client{Timeout: defaultJWKSHTTPTimeout}
		}
		v.jwks = newJWKSCache(cfg.JWKSURL, ttl, client, nowFn)
	}
	return v
}

// JWTMiddleware returns HTTP middleware that validates JWT bearer tokens.
func JWTMiddleware(cfg JWTConfig) func(http.Handler) http.Handler {
	skipSet := make(map[string]bool, len(cfg.SkipPaths))
	for _, p := range cfg.SkipPaths {
		skipSet[p] = true
	}
	refreshSet := make(map[string]bool, len(cfg.AllowExpiredPaths))
	for _, p := range cfg.AllowExpiredPaths {
		refreshSet[p] = true
	}

	verifier := newJWTVerifier(cfg)

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
			claims, err := verifier.verify(r.Context(), token)
			if err != nil {
				http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusUnauthorized)
				return
			}

			// Enforce issuer/audience where configured.
			if cfg.Issuer != "" && claims.Issuer != cfg.Issuer {
				http.Error(w, `{"error":"invalid issuer"}`, http.StatusUnauthorized)
				return
			}
			if cfg.Audience != "" && !claims.Audience.Contains(cfg.Audience) {
				http.Error(w, `{"error":"invalid audience"}`, http.StatusUnauthorized)
				return
			}

			// Expiry: rejected unless this is a refresh path within the grace window.
			if claims.Exp > 0 {
				now := verifier.nowFn().Unix()
				if now > claims.Exp {
					grace := int64(0)
					if refreshSet[r.URL.Path] && cfg.AllowExpiredAt > 0 {
						grace = int64(cfg.AllowExpiredAt.Seconds())
					}
					if now > claims.Exp+grace {
						http.Error(w, `{"error":"token expired"}`, http.StatusUnauthorized)
						return
					}
				}
			}

			ctx := context.WithValue(r.Context(), userContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// verify selects a verification path based on the token's "alg" header and
// returns the decoded claims. Signature and structural validation happen here;
// issuer/audience/expiry are enforced by the caller.
func (v *jwtVerifier) verify(ctx context.Context, token string) (*UserClaims, error) {
	alg, kid, err := parseJOSEHeader(token)
	if err != nil {
		return nil, err
	}

	switch alg {
	case "HS256":
		if v.cfg.Secret == "" {
			return nil, fmt.Errorf("HS256 tokens not accepted")
		}
		return validateJWT(token, v.cfg.Secret)
	case "RS256", "RS384", "RS512":
		pub, err := v.rsaKey(ctx, kid)
		if err != nil {
			return nil, err
		}
		return verifyRSA(token, pub, alg)
	default:
		return nil, fmt.Errorf("unsupported algorithm %q", alg)
	}
}

// rsaKey resolves the RSA public key for RS* verification, preferring JWKS
// (by kid, with rotation) and falling back to a configured static key.
func (v *jwtVerifier) rsaKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	if v.jwks != nil {
		pub, err := v.jwks.key(ctx, kid)
		if err == nil {
			return pub, nil
		}
		if v.cfg.RSAPublicKey != nil {
			return v.cfg.RSAPublicKey, nil
		}
		return nil, err
	}
	if v.cfg.RSAPublicKey != nil {
		return v.cfg.RSAPublicKey, nil
	}
	return nil, fmt.Errorf("no RSA key source configured")
}

// parseJOSEHeader decodes the token's protected header and returns alg/kid.
func parseJOSEHeader(token string) (alg, kid string, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", "", fmt.Errorf("invalid token format")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", "", fmt.Errorf("invalid header encoding: %w", err)
	}
	var hdr struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerBytes, &hdr); err != nil {
		return "", "", fmt.Errorf("invalid header: %w", err)
	}
	if hdr.Alg == "" {
		return "", "", fmt.Errorf("missing algorithm in header")
	}
	return hdr.Alg, hdr.Kid, nil
}

// validateJWT verifies an HS256-signed token and returns its claims. Kept
// unchanged for backward compatibility.
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

	claims, err := decodeClaims(payloadBytes)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

// verifyRSA verifies an RS256/384/512-signed token against an RSA public key.
func verifyRSA(token string, pub *rsa.PublicKey, alg string) (*UserClaims, error) {
	if pub == nil {
		return nil, fmt.Errorf("no RSA public key available")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid payload encoding: %w", err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("invalid signature encoding: %w", err)
	}

	var hashFn crypto.Hash
	switch alg {
	case "RS256":
		hashFn = crypto.SHA256
	case "RS384":
		hashFn = crypto.SHA384
	case "RS512":
		hashFn = crypto.SHA512
	default:
		return nil, fmt.Errorf("unsupported algorithm %q", alg)
	}

	digest := hashDigest(hashFn, parts[0]+"."+parts[1])
	if verifyErr := rsa.VerifyPKCS1v15(pub, hashFn, digest, sig); verifyErr != nil {
		return nil, fmt.Errorf("invalid signature")
	}

	claims, err := decodeClaims(payloadBytes)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

func hashDigest(h crypto.Hash, input string) []byte {
	switch h {
	case crypto.SHA384:
		d := sha512.Sum384([]byte(input))
		return d[:]
	case crypto.SHA512:
		d := sha512.Sum512([]byte(input))
		return d[:]
	default:
		d := sha256.Sum256([]byte(input))
		return d[:]
	}
}

// decodeClaims unmarshals the JWT payload and normalizes subject into UserID.
func decodeClaims(payload []byte) (*UserClaims, error) {
	var claims UserClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("invalid claims: %w", err)
	}
	if claims.UserID == "" && claims.Subject != "" {
		claims.UserID = claims.Subject
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
