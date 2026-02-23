// Package chronosos provides the ChronosOS control plane server.
package chronosos

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/chronos-ai/chronos/engine/stream"
	"github.com/chronos-ai/chronos/os/approval"
	"github.com/chronos-ai/chronos/os/auth"
	"github.com/chronos-ai/chronos/os/trace"
	"github.com/chronos-ai/chronos/storage"
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
	// TODO: implement with query params
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, `{"sessions":[]}`)
}

func (s *Server) handleListTraces(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, `{"traces":[]}`)
}

// Start begins serving the control plane.
func (s *Server) Start(_ context.Context) error {
	log.Printf("ChronosOS starting on %s", s.Addr)
	return http.ListenAndServe(s.Addr, s.mux)
}
