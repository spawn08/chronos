package builtins

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spawn08/chronos/engine/tool"
)

// NewHTTPTool creates a tool that makes HTTP requests.
// timeout controls max request time (0 = 30s default).
// maxBodySize limits response body read size (0 = 1MB default).
func NewHTTPTool(timeout time.Duration, maxBodySize int64) *tool.Definition {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if maxBodySize <= 0 {
		maxBodySize = 1 << 20
	}

	client := &http.Client{Timeout: timeout}

	return &tool.Definition{
		Name:        "http_request",
		Description: "Make an HTTP request and return the response status, headers, and body.",
		Permission:  tool.PermRequireApproval,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The URL to request",
				},
				"method": map[string]any{
					"type":        "string",
					"description": "HTTP method (GET, POST, PUT, DELETE, etc.). Defaults to GET.",
				},
				"body": map[string]any{
					"type":        "string",
					"description": "Request body (for POST/PUT)",
				},
				"headers": map[string]any{
					"type":        "object",
					"description": "Request headers as key-value pairs",
				},
			},
			"required": []string{"url"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			url, ok := args["url"].(string)
			if !ok || url == "" {
				return nil, fmt.Errorf("http_request: 'url' argument is required")
			}

			method := "GET"
			if m, ok := args["method"].(string); ok && m != "" {
				method = strings.ToUpper(m)
			}

			var bodyReader io.Reader
			if body, ok := args["body"].(string); ok && body != "" {
				bodyReader = strings.NewReader(body)
			}

			req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
			if err != nil {
				return nil, fmt.Errorf("http_request: %w", err)
			}

			if headers, ok := args["headers"].(map[string]any); ok {
				for k, v := range headers {
					if vs, ok := v.(string); ok {
						req.Header.Set(k, vs)
					}
				}
			}

			resp, err := client.Do(req)
			if err != nil {
				return nil, fmt.Errorf("http_request: %w", err)
			}
			defer resp.Body.Close()

			bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
			if err != nil {
				return nil, fmt.Errorf("http_request: reading body: %w", err)
			}

			respHeaders := make(map[string]string)
			for k := range resp.Header {
				respHeaders[k] = resp.Header.Get(k)
			}

			return map[string]any{
				"status_code": resp.StatusCode,
				"headers":     respHeaders,
				"body":        string(bodyBytes),
			}, nil
		},
	}
}
