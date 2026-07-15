package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

const (
	// defaultJWKSCacheTTL bounds how long a fetched JWKS is trusted before a
	// background-free lazy refresh on the next request.
	defaultJWKSCacheTTL = 15 * time.Minute
	// defaultJWKSHTTPTimeout is the timeout for JWKS endpoint fetches.
	defaultJWKSHTTPTimeout = 10 * time.Second
	// jwksMissCooldown throttles re-fetches triggered by an unknown kid so an
	// attacker cannot force unbounded upstream requests, while still allowing
	// key rotation to be picked up promptly.
	jwksMissCooldown = 30 * time.Second
	// maxJWKSBody caps the JWKS response size to guard against oversized bodies.
	maxJWKSBody = 1 << 20 // 1 MiB
)

// jwkKey is a single RSA key entry in a JWKS document.
type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwksDoc struct {
	Keys []jwkKey `json:"keys"`
}

// jwksCache fetches and caches RSA public keys from a JWKS endpoint, indexed by
// "kid". It is safe for concurrent use and honors key rotation by re-fetching
// on a cache miss (throttled) or when the cached set is stale.
type jwksCache struct {
	url        string
	ttl        time.Duration
	httpClient *http.Client
	now        func() time.Time

	mu           sync.RWMutex
	keys         map[string]*rsa.PublicKey
	fetchedAt    time.Time
	lastMissTime time.Time
}

func newJWKSCache(url string, ttl time.Duration, client *http.Client, now func() time.Time) *jwksCache {
	return &jwksCache{
		url:        url,
		ttl:        ttl,
		httpClient: client,
		now:        now,
		keys:       make(map[string]*rsa.PublicKey),
	}
}

// key returns the RSA public key for the given kid, fetching or refreshing the
// JWKS as needed. When kid is empty and exactly one key is cached, that key is
// returned.
func (c *jwksCache) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	if pub, ok := c.lookup(kid); ok {
		return pub, nil
	}

	// Cache miss or stale: refresh (throttled for unknown-kid misses).
	if err := c.refresh(ctx); err != nil {
		return nil, err
	}

	if pub, ok := c.lookup(kid); ok {
		return pub, nil
	}
	if kid == "" {
		return nil, fmt.Errorf("no usable JWKS key found")
	}
	return nil, fmt.Errorf("no JWKS key for kid %q", kid)
}

// lookup returns a cached key if present and not stale.
func (c *jwksCache) lookup(kid string) (*rsa.PublicKey, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.now().Sub(c.fetchedAt) >= c.ttl {
		return nil, false
	}
	if kid == "" {
		if len(c.keys) == 1 {
			for _, v := range c.keys {
				return v, true
			}
		}
		return nil, false
	}
	pub, ok := c.keys[kid]
	return pub, ok
}

// refresh re-fetches the JWKS. Misses within jwksMissCooldown of a prior fetch
// are skipped unless the cache is stale, to bound upstream traffic.
func (c *jwksCache) refresh(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	stale := now.Sub(c.fetchedAt) >= c.ttl
	if !stale && now.Sub(c.lastMissTime) < jwksMissCooldown && !c.fetchedAt.IsZero() {
		// Recently attempted; avoid hammering the endpoint on repeated misses.
		return fmt.Errorf("JWKS refresh throttled")
	}
	c.lastMissTime = now

	keys, err := fetchJWKS(ctx, c.httpClient, c.url)
	if err != nil {
		return err
	}
	c.keys = keys
	c.fetchedAt = now
	return nil
}

func fetchJWKS(ctx context.Context, client *http.Client, url string) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build JWKS request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch JWKS: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch JWKS: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSBody))
	if err != nil {
		return nil, fmt.Errorf("read JWKS body: %w", err)
	}

	var doc jwksDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse JWKS: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "RSA" {
			continue
		}
		if k.Use != "" && k.Use != "sig" {
			continue
		}
		pub, err := k.rsaPublicKey()
		if err != nil {
			return nil, fmt.Errorf("decode JWKS key %q: %w", k.Kid, err)
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("JWKS contained no usable RSA keys")
	}
	return keys, nil
}

// rsaPublicKey reconstructs an RSA public key from the JWK modulus/exponent.
func (k jwkKey) rsaPublicKey() (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("exponent: %w", err)
	}
	if len(nBytes) == 0 || len(eBytes) == 0 {
		return nil, fmt.Errorf("empty modulus or exponent")
	}
	// Bound the exponent length before the fixed-width copy below: an exponent
	// longer than 8 bytes would make 8-len(eBytes) negative and panic. A hostile
	// or misconfigured JWKS endpoint must not be able to crash the verifier.
	if len(eBytes) > 8 {
		return nil, fmt.Errorf("exponent too large")
	}

	n := new(big.Int).SetBytes(nBytes)

	// Exponent is a big-endian unsigned integer; left-pad to 8 bytes.
	var eBuf [8]byte
	copy(eBuf[8-len(eBytes):], eBytes)
	e := binary.BigEndian.Uint64(eBuf[:])
	if e == 0 || e > 1<<31 {
		return nil, fmt.Errorf("invalid exponent")
	}

	return &rsa.PublicKey{N: n, E: int(e)}, nil
}
