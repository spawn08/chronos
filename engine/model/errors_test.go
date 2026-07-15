package model

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"circuit open", ErrCircuitOpen, false},
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"429", &APIError{StatusCode: http.StatusTooManyRequests}, true},
		{"408", &APIError{StatusCode: http.StatusRequestTimeout}, true},
		{"500", &APIError{StatusCode: 500}, true},
		{"503", &APIError{StatusCode: 503}, true},
		{"400", &APIError{StatusCode: 400}, false},
		{"401", &APIError{StatusCode: 401}, false},
		{"404", &APIError{StatusCode: 404}, false},
		{"wrapped 500", fmt.Errorf("provider: %w", &APIError{StatusCode: 500}), true},
		{"plain network error", errors.New("dial tcp: connection refused"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(tt.err); got != tt.want {
				t.Errorf("IsRetryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsTerminal(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"400", &APIError{StatusCode: 400}, true},
		{"401", &APIError{StatusCode: 401}, true},
		{"404", &APIError{StatusCode: 404}, true},
		{"422", &APIError{StatusCode: 422}, true},
		{"429 not terminal", &APIError{StatusCode: http.StatusTooManyRequests}, false},
		{"408 not terminal", &APIError{StatusCode: http.StatusRequestTimeout}, false},
		{"500 not terminal", &APIError{StatusCode: 500}, false},
		{"wrapped 403", fmt.Errorf("x: %w", &APIError{StatusCode: 403}), true},
		{"plain error", errors.New("boom"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTerminal(tt.err); got != tt.want {
				t.Errorf("IsTerminal(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	future := time.Now().Add(45 * time.Second).UTC().Format(http.TimeFormat)
	past := time.Now().Add(-1 * time.Hour).UTC().Format(http.TimeFormat)
	tests := []struct {
		name    string
		in      string
		wantMin time.Duration
		wantMax time.Duration
	}{
		{"empty", "", 0, 0},
		{"seconds", "5", 5 * time.Second, 5 * time.Second},
		{"zero seconds", "0", 0, 0},
		{"negative seconds", "-3", 0, 0},
		{"garbage", "soon", 0, 0},
		{"http date future", future, 30 * time.Second, 46 * time.Second},
		{"http date past", past, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRetryAfter(tt.in)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("parseRetryAfter(%q) = %v, want in [%v,%v]", tt.in, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestAPIError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *APIError
		want string
	}{
		{"with body", &APIError{Status: "429 Too Many Requests", Body: "slow down"}, "429 Too Many Requests: slow down"},
		{"no body", &APIError{Status: "500 Internal Server Error"}, "500 Internal Server Error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}
