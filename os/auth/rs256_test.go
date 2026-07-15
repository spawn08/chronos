package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- RS256 test helpers -----------------------------------------------------

func newRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

// signRS256 produces an RS256 JWT with the given kid and claims.
func signRS256(t *testing.T, key *rsa.PrivateKey, kid string, claims UserClaims) string {
	t.Helper()
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	if kid != "" {
		header["kid"] = kid
	}
	hBytes, _ := json.Marshal(header)
	pBytes, _ := json.Marshal(claims)
	hEnc := base64.RawURLEncoding.EncodeToString(hBytes)
	pEnc := base64.RawURLEncoding.EncodeToString(pBytes)
	signingInput := hEnc + "." + pEnc

	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// jwksHandlerFor serves a JWKS document exposing the given public keys by kid.
func jwksJSON(keys map[string]*rsa.PublicKey) []byte {
	type jwk struct {
		Kty string `json:"kty"`
		Kid string `json:"kid"`
		Use string `json:"use"`
		Alg string `json:"alg"`
		N   string `json:"n"`
		E   string `json:"e"`
	}
	var doc struct {
		Keys []jwk `json:"keys"`
	}
	for kid, pub := range keys {
		eBytes := big.NewInt(int64(pub.E)).Bytes()
		doc.Keys = append(doc.Keys, jwk{
			Kty: "RSA",
			Kid: kid,
			Use: "sig",
			Alg: "RS256",
			N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			E:   base64.RawURLEncoding.EncodeToString(eBytes),
		})
	}
	b, _ := json.Marshal(doc)
	return b
}

// --- RS256 / JWKS verification ---------------------------------------------

func TestJWTMiddleware_RS256_JWKS(t *testing.T) {
	key := newRSAKey(t)
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		_, _ = w.Write(jwksJSON(map[string]*rsa.PublicKey{"k1": &key.PublicKey}))
	}))
	defer srv.Close()

	token := signRS256(t, key, "k1", UserClaims{
		Subject: "oidc-user",
		Roles:   []string{"admin"},
		Exp:     time.Now().Add(time.Hour).Unix(),
	})

	handler := JWTMiddleware(JWTConfig{JWKSURL: srv.URL})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := UserFromContext(r.Context())
			if !ok || u.UserID != "oidc-user" {
				t.Errorf("bad claims: %+v ok=%v", u, ok)
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	// Two requests should hit JWKS only once (cache).
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/test", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("req %d: got %d, want 200 (%s)", i, rec.Code, rec.Body.String())
		}
	}
	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Errorf("JWKS fetched %d times, want 1 (cache)", got)
	}
}

func TestJWTMiddleware_RS256_UnknownKidRejected(t *testing.T) {
	key := newRSAKey(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(jwksJSON(map[string]*rsa.PublicKey{"k1": &key.PublicKey}))
	}))
	defer srv.Close()

	// Token signed with a kid that JWKS does not serve.
	token := signRS256(t, key, "unknown", UserClaims{Subject: "u", Exp: time.Now().Add(time.Hour).Unix()})

	handler := JWTMiddleware(JWTConfig{JWKSURL: srv.URL})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	)
	req := httptest.NewRequest("GET", "/api/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rec.Code)
	}
}

func TestJWTMiddleware_RS256_WrongKeyRejected(t *testing.T) {
	signer := newRSAKey(t)
	other := newRSAKey(t)
	// JWKS serves a DIFFERENT public key than the one that signed the token.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(jwksJSON(map[string]*rsa.PublicKey{"k1": &other.PublicKey}))
	}))
	defer srv.Close()

	token := signRS256(t, signer, "k1", UserClaims{Subject: "u", Exp: time.Now().Add(time.Hour).Unix()})
	handler := JWTMiddleware(JWTConfig{JWKSURL: srv.URL})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	)
	req := httptest.NewRequest("GET", "/api/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rec.Code)
	}
}

func TestJWKSCache_Rotation(t *testing.T) {
	oldKey := newRSAKey(t)
	newKey := newRSAKey(t)

	var mu sync.Mutex
	current := map[string]*rsa.PublicKey{"k1": &oldKey.PublicKey}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = w.Write(jwksJSON(current))
	}))
	defer srv.Close()

	// Cache with a controllable clock and no miss cooldown effect (fresh start).
	clock := &fakeClock{t: time.Now()}
	cache := newJWKSCache(srv.URL, time.Hour, srv.Client(), clock.now)

	// Resolve old key.
	if _, err := cache.key(context.Background(), "k1"); err != nil {
		t.Fatalf("resolve old key: %v", err)
	}

	// Rotate: server now serves a new key under a new kid.
	mu.Lock()
	current = map[string]*rsa.PublicKey{"k2": &newKey.PublicKey}
	mu.Unlock()

	// Advance past the miss cooldown so the unknown-kid miss triggers a refetch.
	clock.advance(jwksMissCooldown + time.Second)

	pub, err := cache.key(context.Background(), "k2")
	if err != nil {
		t.Fatalf("resolve rotated key: %v", err)
	}
	if pub.N.Cmp(newKey.PublicKey.N) != 0 {
		t.Error("rotated key mismatch")
	}
}

// --- issuer / audience enforcement -----------------------------------------

func TestJWTMiddleware_IssuerAudience(t *testing.T) {
	tests := []struct {
		name        string
		cfgIssuer   string
		cfgAudience string
		claimIss    string
		claimAud    Audience
		wantStatus  int
	}{
		{"match", "https://issuer", "chronos", "https://issuer", Audience{"chronos"}, http.StatusOK},
		{"aud array match", "https://issuer", "chronos", "https://issuer", Audience{"other", "chronos"}, http.StatusOK},
		{"wrong issuer", "https://issuer", "", "https://evil", nil, http.StatusUnauthorized},
		{"wrong audience", "", "chronos", "", Audience{"nope"}, http.StatusUnauthorized},
		{"no enforcement", "", "", "whatever", Audience{"whatever"}, http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			claims := UserClaims{UserID: "u", Exp: time.Now().Add(time.Hour).Unix(), Issuer: tc.claimIss, Audience: tc.claimAud}
			token := CreateTestToken(claims, testSecret)
			handler := JWTMiddleware(JWTConfig{Secret: testSecret, Issuer: tc.cfgIssuer, Audience: tc.cfgAudience})(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
			)
			req := httptest.NewRequest("GET", "/api/test", http.NoBody)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Errorf("got %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

// --- refresh-flow AllowExpired ---------------------------------------------

func TestJWTMiddleware_AllowExpiredRefreshFlow(t *testing.T) {
	// Token expired 30s ago.
	claims := UserClaims{UserID: "u", Exp: time.Now().Add(-30 * time.Second).Unix()}
	token := CreateTestToken(claims, testSecret)

	cfg := JWTConfig{
		Secret:            testSecret,
		AllowExpiredAt:    2 * time.Minute,
		AllowExpiredPaths: []string{"/auth/refresh"},
	}
	handler := JWTMiddleware(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	tests := []struct {
		path       string
		wantStatus int
	}{
		{"/auth/refresh", http.StatusOK},            // within grace on a refresh path
		{"/api/protected", http.StatusUnauthorized}, // grace does not apply elsewhere
	}
	for _, tc := range tests {
		req := httptest.NewRequest("GET", tc.path, http.NoBody)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.wantStatus {
			t.Errorf("path %s: got %d, want %d", tc.path, rec.Code, tc.wantStatus)
		}
	}
}

func TestJWTMiddleware_AllowExpiredBeyondGraceRejected(t *testing.T) {
	// Expired well beyond the grace window.
	claims := UserClaims{UserID: "u", Exp: time.Now().Add(-10 * time.Minute).Unix()}
	token := CreateTestToken(claims, testSecret)
	cfg := JWTConfig{Secret: testSecret, AllowExpiredAt: time.Minute, AllowExpiredPaths: []string{"/auth/refresh"}}
	handler := JWTMiddleware(cfg)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	)
	req := httptest.NewRequest("GET", "/auth/refresh", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rec.Code)
	}
}

func TestJWTMiddleware_RejectsNoneAlg(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"user_id":"u"}`))
	token := header + "." + payload + "."
	handler := JWTMiddleware(JWTConfig{Secret: testSecret})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	)
	req := httptest.NewRequest("GET", "/api/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rec.Code)
	}
}

// fakeClock is a controllable time source for cache tests.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}
