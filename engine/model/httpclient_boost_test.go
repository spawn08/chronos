package model

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

type marshalFail struct{}

func (marshalFail) MarshalJSON() ([]byte, error) {
	return nil, errors.New("marshal blocked")
}

func TestHTTPClient_post_MarshalError_Boost(t *testing.T) {
	h := newHTTPClient("http://example.com", 5, nil)
	_, err := h.post(context.Background(), "/x", marshalFail{})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestDrainAndClose_Boost(t *testing.T) {
	rc := io.NopCloser(bytes.NewBufferString("leftover"))
	drainAndClose(rc)
	// Second close should not panic
	drainAndClose(io.NopCloser(bytes.NewReader(nil)))
}
