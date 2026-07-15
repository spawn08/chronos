package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"net/http"
	"time"
)

// Tuned connection-pool and timeout defaults for the shared transport. LLM APIs
// are called concurrently and benefit from a large keep-alive pool; the stdlib
// default of 2 idle conns/host causes connection churn under load.
const (
	defaultConnectTimeout      = 10 * time.Second
	defaultKeepAlive           = 30 * time.Second
	defaultMaxIdleConns        = 100
	defaultMaxIdleConnsPerHost = 100
	defaultIdleConnTimeout     = 90 * time.Second
	defaultTLSHandshakeTimeout = 10 * time.Second
	defaultBackoffBase         = 500 * time.Millisecond
	defaultBackoffMax          = 30 * time.Second
	defaultBreakerThreshold    = 5
	defaultBreakerCooldown     = 30 * time.Second
)

// httpClient is a shared helper for making HTTP requests to LLM APIs. It owns a
// tuned connection pool, retry/backoff with circuit breaking, and separate
// clients for unary vs. streaming requests (streaming must not be cut off by an
// overall request deadline).
type httpClient struct {
	client       *http.Client // unary requests: bounded by an overall timeout
	streamClient *http.Client // streaming requests: no overall timeout
	transport    *http.Transport
	baseURL      string
	headers      map[string]string

	maxRetries  int
	breaker     *circuitBreaker
	backoffBase time.Duration
	backoffMax  time.Duration
	// sleep waits for d or until ctx is done; injectable for tests.
	sleep func(ctx context.Context, d time.Duration) error
}

// httpOption customizes an httpClient at construction time.
type httpOption func(*httpClient)

// withMaxRetries sets the maximum number of retry attempts for transient
// failures. When >0 a per-provider circuit breaker is enabled by default.
func withMaxRetries(n int) httpOption {
	return func(h *httpClient) {
		if n > 0 {
			h.maxRetries = n
		}
	}
}

// withCircuitBreaker installs a specific circuit breaker (overriding the
// default that withMaxRetries would otherwise create).
func withCircuitBreaker(cb *circuitBreaker) httpOption {
	return func(h *httpClient) { h.breaker = cb }
}

// withSleepFn overrides the backoff sleep function (used in tests to avoid real
// delays).
func withSleepFn(fn func(ctx context.Context, d time.Duration) error) httpOption {
	return func(h *httpClient) {
		if fn != nil {
			h.sleep = fn
		}
	}
}

func newHTTPClient(baseURL string, timeoutSec int, headers map[string]string, opts ...httpOption) *httpClient {
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	total := time.Duration(timeoutSec) * time.Second

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   defaultConnectTimeout, // connect timeout, separate from total
			KeepAlive: defaultKeepAlive,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          defaultMaxIdleConns,
		MaxIdleConnsPerHost:   defaultMaxIdleConnsPerHost,
		MaxConnsPerHost:       0, // unlimited concurrent connections per host
		IdleConnTimeout:       defaultIdleConnTimeout,
		TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: total, // bound time-to-first-byte for unary and streaming
	}

	h := &httpClient{
		// Unary client: overall Timeout covers the full request/response.
		client: &http.Client{Timeout: total, Transport: transport},
		// Streaming client: no overall Timeout so long-lived streams are not cut
		// off; time-to-first-byte is still bounded by ResponseHeaderTimeout and
		// the caller's context governs the stream lifetime.
		streamClient: &http.Client{Transport: transport},
		transport:    transport,
		baseURL:      baseURL,
		headers:      headers,
		backoffBase:  defaultBackoffBase,
		backoffMax:   defaultBackoffMax,
		sleep:        sleepWithContext,
	}
	for _, opt := range opts {
		opt(h)
	}
	// Enable a per-provider circuit breaker whenever retries are configured,
	// unless the caller supplied one explicitly.
	if h.breaker == nil && h.maxRetries > 0 {
		h.breaker = newCircuitBreaker(defaultBreakerThreshold, defaultBreakerCooldown)
	}
	return h
}

// post issues a unary POST with retry/backoff and circuit breaking. It returns
// the final *http.Response so callers can inspect the status and body exactly
// as before; retries for transient failures happen transparently.
func (h *httpClient) post(ctx context.Context, path string, body any) (*http.Response, error) {
	return h.doWithRetry(ctx, h.client, path, body)
}

// postStream issues a POST intended for a streaming (SSE) response. It uses the
// streaming client (no overall timeout) so the connection survives for the
// duration of the stream.
func (h *httpClient) postStream(ctx context.Context, path string, body any) (*http.Response, error) {
	return h.doWithRetry(ctx, h.streamClient, path, body)
}

func (h *httpClient) doWithRetry(ctx context.Context, client *http.Client, path string, body any) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	if h.breaker != nil && !h.breaker.allow() {
		return nil, fmt.Errorf("http request: %w", ErrCircuitOpen)
	}

	attempts := h.maxRetries + 1
	for attempt := 0; ; attempt++ {
		resp, doErr := h.doOnce(ctx, client, path, payload)
		if doErr != nil {
			// Network/transport failure: retry unless the context is done or
			// retries are exhausted.
			if attempt < attempts-1 && ctx.Err() == nil {
				if werr := h.waitBackoff(ctx, attempt, 0); werr != nil {
					h.recordBreaker(false)
					return nil, fmt.Errorf("http request: %w", werr)
				}
				continue
			}
			h.recordBreaker(false)
			return nil, fmt.Errorf("http request: %w", doErr)
		}

		if isRetryableStatus(resp.StatusCode) && attempt < attempts-1 {
			retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
			drainAndClose(resp.Body)
			if werr := h.waitBackoff(ctx, attempt, retryAfter); werr != nil {
				h.recordBreaker(false)
				return nil, fmt.Errorf("http request: %w", werr)
			}
			continue
		}

		// Terminal outcome: success, a non-retryable status, or retries
		// exhausted. Only upstream availability failures trip the breaker;
		// 4xx client errors (except 429) count as healthy.
		h.recordBreaker(!isServerFailure(resp.StatusCode))
		return resp, nil
	}
}

func (h *httpClient) doOnce(ctx context.Context, client *http.Client, path string, payload []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}
	return client.Do(req)
}

func (h *httpClient) recordBreaker(success bool) {
	if h.breaker == nil {
		return
	}
	if success {
		h.breaker.recordSuccess()
	} else {
		h.breaker.recordFailure()
	}
}

// waitBackoff waits before the next retry, honoring a server-provided
// Retry-After delay when present.
func (h *httpClient) waitBackoff(ctx context.Context, attempt int, retryAfter time.Duration) error {
	delay := retryAfter
	if delay <= 0 {
		delay = h.backoffDelay(attempt)
	}
	sleep := h.sleep
	if sleep == nil {
		sleep = sleepWithContext
	}
	return sleep(ctx, delay)
}

// backoffDelay computes an exponential backoff with ±25% jitter, capped at
// backoffMax. attempt is 0-based.
func (h *httpClient) backoffDelay(attempt int) time.Duration {
	base := float64(h.backoffBase)
	if base <= 0 {
		base = float64(defaultBackoffBase)
	}
	maxD := float64(h.backoffMax)
	if maxD <= 0 {
		maxD = float64(defaultBackoffMax)
	}
	delay := base * math.Pow(2, float64(attempt))
	delay += delay * 0.25 * (rand.Float64()*2 - 1) //nolint:gosec // jitter, not security-sensitive
	if delay > maxD {
		delay = maxD
	}
	if delay < 0 {
		delay = 0
	}
	return time.Duration(delay)
}

// sleepWithContext sleeps for d or returns early if ctx is canceled.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func drainAndClose(r io.ReadCloser) {
	_, _ = io.Copy(io.Discard, r)
	r.Close()
}

func readErrorBody(resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return resp.Status
	}
	return fmt.Sprintf("%s: %s", resp.Status, string(body))
}

// newAPIError builds an APIError from a non-2xx response, reading a bounded
// portion of the body. The caller retains ownership of resp.Body and should
// drain/close it afterwards.
func newAPIError(resp *http.Response) *APIError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return &APIError{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Body:       string(body),
		RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
	}
}
