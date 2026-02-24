// Package chronosos provides the ChronosOS control plane server.
package chronosos

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/spawn08/chronos/engine/stream"
	"github.com/spawn08/chronos/os/approval"
	"github.com/spawn08/chronos/os/auth"
	"github.com/spawn08/chronos/os/trace"
	"github.com/spawn08/chronos/storage"
)

// Server is the ChronosOS control plane.
type Server struct {
	Addr     string
	Store    storage.Storage
	Broker   *stream.Broker
	Auth     *auth.Service
	Trace    *trace.Collector
	Approval *approval.Service
	mux      *http.ServeMux
}

// New creates a new ChronosOS server.
func New(addr string, store storage.Storage) *Server {
	s := &Server{
		Addr:     addr,
		Store:    store,
		Broker:   stream.NewBroker(),
		Auth:     auth.NewService(),
		Trace:    trace.NewCollector(store),
		Approval: approval.NewService(),
		mux:      http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	})
	s.mux.HandleFunc("/api/sessions", s.handleListSessions)
	s.mux.HandleFunc("/api/traces", s.handleListTraces)
	s.mux.HandleFunc("/api/events/stream", s.Broker.SSEHandler("dashboard"))
	s.mux.HandleFunc("/api/approval/pending", s.Approval.HandlePending)
	s.mux.HandleFunc("/api/approval/respond", s.Approval.HandleRespond)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent_id")
	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	sessions, err := s.Store.ListSessions(r.Context(), agentID, limit, offset)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"sessions": sessions})
}

func (s *Server) handleListTraces(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"traces":[],"error":"session_id query parameter is required"}`)
		return
	}
	traces, err := s.Store.ListTraces(r.Context(), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"traces": traces})
}

// Start begins serving the control plane.
func (s *Server) Start(_ context.Context) error {
	log.Printf("ChronosOS starting on %s", s.Addr)
	return http.ListenAndServe(s.Addr, s.mux)
}
