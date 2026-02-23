package model

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// httpClient is a shared helper for making HTTP requests to LLM APIs.
type httpClient struct {
	client  *http.Client
	baseURL string
	headers map[string]string
}

func newHTTPClient(baseURL string, timeoutSec int, headers map[string]string) *httpClient {
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	return &httpClient{
		client: &http.Client{
			Timeout: time.Duration(timeoutSec) * time.Second,
		},
		baseURL: baseURL,
		headers: headers,
	}
}

func (h *httpClient) post(ctx context.Context, path string, body any) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}

	return resp, nil
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
