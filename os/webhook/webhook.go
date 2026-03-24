// Package webhook provides a generic webhook interface for receiving external events.
package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// Event represents an incoming webhook event.
type Event struct {
	Source  string         `json:"source"`
	Type   string         `json:"type"`
	Body   json.RawMessage `json:"body"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Handler processes webhook events.
type Handler func(ctx context.Context, event Event) error

// Server manages webhook endpoints and routes events to handlers.
type Server struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
	secret   string
	mux      *http.ServeMux
}

// NewServer creates a webhook server.
// secret is optional; if provided, it validates X-Webhook-Secret header.
func NewServer(secret string) *Server {
	s := &Server{
		handlers: make(map[string][]Handler),
		secret:   secret,
		mux:      http.NewServeMux(),
	}
	s.mux.HandleFunc("/webhook", s.handleWebhook)
	s.mux.HandleFunc("/webhook/", s.handleWebhook)
	return s
}

// On registers a handler for a specific event type.
func (s *Server) On(eventType string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[eventType] = append(s.handlers[eventType], h)
}

// Handler returns the HTTP handler for mounting.
func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.secret != "" {
		if r.Header.Get("X-Webhook-Secret") != s.secret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	eventType := r.Header.Get("X-Event-Type")
	if eventType == "" {
		eventType = "generic"
	}

	headers := make(map[string]string)
	for k := range r.Header {
		headers[k] = r.Header.Get(k)
	}

	event := Event{
		Source:  r.Header.Get("X-Event-Source"),
		Type:   eventType,
		Body:   json.RawMessage(body),
		Headers: headers,
	}

	s.mu.RLock()
	handlers := s.handlers[eventType]
	wildcardHandlers := s.handlers["*"]
	s.mu.RUnlock()

	allHandlers := append(handlers, wildcardHandlers...)

	var errs []error
	for _, h := range allHandlers {
		if err := h(r.Context(), event); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		http.Error(w, fmt.Sprintf("handler errors: %v", errs), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
