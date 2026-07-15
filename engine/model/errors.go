package model

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrCircuitOpen is returned when a provider's circuit breaker is open and
// requests are short-circuited to give the upstream time to recover.
var ErrCircuitOpen = errors.New("circuit breaker open")

// APIError represents a non-2xx HTTP response from a model provider API. It
// carries the status code, body snippet and any server-requested retry delay so
// callers (retry logic, fallback routing, circuit breakers) can classify the
// failure as retryable or terminal.
type APIError struct {
	StatusCode int           `json:"status_code"`
	Status     string        `json:"status"`
	Body       string        `json:"body"`
	RetryAfter time.Duration `json:"retry_after"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("%s: %s", e.Status, e.Body)
	}
	return e.Status
}

// IsRetryable reports whether err represents a transient failure worth retrying
// (network/transport failure, HTTP 408, 429 or 5xx). Context cancellation,
// deadline expiry and an open circuit are not retryable.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrCircuitOpen) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return isRetryableStatus(apiErr.StatusCode)
	}
	// Non-API errors are typically network/transport failures → retryable.
	return true
}

// IsTerminal reports whether err is a permanent failure — a 4xx client error
// other than 408/429 — that will not succeed on retry or on a different
// provider (e.g. malformed request, authentication failure, not found).
func IsTerminal(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 &&
			apiErr.StatusCode != http.StatusTooManyRequests &&
			apiErr.StatusCode != http.StatusRequestTimeout
	}
	return false
}

// isRetryableStatus reports whether an HTTP status code warrants a retry.
func isRetryableStatus(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusRequestTimeout ||
		code >= 500
}

// isServerFailure reports whether a status code represents an upstream
// availability failure (as opposed to a client error). Used to drive the
// circuit breaker: 4xx client errors must not trip the breaker.
func isServerFailure(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

// parseRetryAfter parses an HTTP Retry-After header value, which may be either
// an integer number of seconds or an HTTP-date. It returns 0 if the value is
// absent, malformed or already in the past.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
