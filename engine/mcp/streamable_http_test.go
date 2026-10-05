package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type streamableFake struct {
	mu       sync.Mutex
	sessions int
	deleted  bool
	auth     []string
	sse      bool
}

func (f *streamableFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method == http.MethodDelete {
		f.deleted = r.Header.Get(headerSessionID) == "sess-1"
		w.WriteHeader(http.StatusOK)
		return
	}
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	var req struct {
		ID     *int64 `json:"id"`
		Method string `json:"method"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if req.Method != "initialize" && r.Header.Get(headerSessionID) != "sess-1" {
		http.Error(w, "no session", http.StatusNotFound)
		return
	}
	var result string
	switch req.Method {
	case "initialize":
		w.Header().Set(headerSessionID, "sess-1")
		result = `{"protocolVersion":"2025-03-26","serverInfo":{"name":"fake","version":"1"}}`
	case "tools/list":
		result = `{"tools":[{"name":"echo","description":"d","inputSchema":{"type":"object"}}]}`
	default:
		result = `{"content":[{"type":"text","text":"ok"}]}`
	}
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, *req.ID, result)
	if f.sse && req.Method != "initialize" {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\nevent: message\ndata: %s\n\n", body)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, body)
}

func connectFake(t *testing.T, f *streamableFake, headers map[string]string) *Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c, err := NewClient(ServerConfig{Name: "fake", Transport: "http", URL: srv.URL, Headers: headers})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestStreamableHTTPJSONAndSession(t *testing.T) {
	t.Setenv("FAKE_TOKEN", "s3cret")
	f := &streamableFake{}
	c := connectFake(t, f, map[string]string{"Authorization": "Bearer ${FAKE_TOKEN}"})
	ctx := context.Background()
	tools, err := c.ListTools(ctx)
	if err != nil || len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools=%v err=%v", tools, err)
	}
	out, err := c.CallTool(ctx, "echo", nil)
	if err != nil || out != "ok" {
		t.Fatalf("out=%v err=%v", out, err)
	}
	_ = c.Close()
	_ = c.Close()
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.deleted {
		t.Error("session was not deleted on Close")
	}
	for _, a := range f.auth {
		if a != "Bearer s3cret" {
			t.Fatalf("Authorization = %q", a)
		}
	}
}

func TestStreamableHTTPEventStreamResponse(t *testing.T) {
	c := connectFake(t, &streamableFake{sse: true}, nil)
	defer c.Close()
	out, err := c.CallTool(context.Background(), "echo", nil)
	if err != nil || out != "ok" {
		t.Fatalf("out=%v err=%v", out, err)
	}
}

func TestStreamableHTTPSessionExpired(t *testing.T) {
	f := &streamableFake{}
	c := connectFake(t, f, nil)
	defer c.Close()
	c.pendingMu.Lock()
	c.sessionID = "stale"
	c.pendingMu.Unlock()
	_, err := c.ListTools(context.Background())
	if err == nil || !strings.Contains(err.Error(), "session expired") {
		t.Fatalf("err=%v", err)
	}
}

func TestStreamableHTTPMissingEnv(t *testing.T) {
	_, err := NewClient(ServerConfig{Name: "x", Transport: TransportStreamableHTTP, URL: "http://h", Headers: map[string]string{"A": "${DEFINITELY_UNSET_VAR_XYZ}"}})
	if err == nil || !strings.Contains(err.Error(), "DEFINITELY_UNSET_VAR_XYZ") {
		t.Fatalf("err=%v", err)
	}
}

func TestExpandEnvDefaultAndRedaction(t *testing.T) {
	got, err := ExpandEnv("a-${UNSET_Q:-dflt}-b")
	if err != nil || got != "a-dflt-b" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	s := ServerConfig{Name: "n", URL: "https://u:p@h/x?key=abc", Headers: map[string]string{"Authorization": "Bearer zzz"}}.String()
	for _, leak := range []string{"zzz", "abc", "u:p"} {
		if strings.Contains(s, leak) {
			t.Errorf("String() leaks %q: %s", leak, s)
		}
	}
}
