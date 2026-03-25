// Package chronosos provides the ChronosOS control plane server.
package chronosos

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spawn08/chronos/engine/stream"
	"github.com/spawn08/chronos/os/approval"
	"github.com/spawn08/chronos/os/auth"
	"github.com/spawn08/chronos/os/metrics"
	"github.com/spawn08/chronos/os/scheduler"
	"github.com/spawn08/chronos/os/trace"
	"github.com/spawn08/chronos/storage"
)

// Server is the ChronosOS control plane.
type Server struct {
	Addr            string
	Store           storage.Storage
	Broker          *stream.Broker
	Auth            *auth.Service
	Trace           *trace.Collector
	Approval        *approval.Service
	Metrics         *metrics.Registry
	Scheduler       *scheduler.Scheduler
	ShutdownTimeout time.Duration
	mux             *http.ServeMux
	ready           atomic.Bool
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
		Metrics:  metrics.NewRegistry(),
		Scheduler: scheduler.New(func(_ context.Context, _, _, _ string) error {
			return fmt.Errorf("no agent runner configured")
		}),
		ShutdownTimeout: 15 * time.Second,
		mux:             http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	})

	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/health/live", s.handleLiveness)
	s.mux.HandleFunc("/health/ready", s.handleReadiness)

	s.mux.HandleFunc("/api/sessions", s.handleListSessions)
	s.mux.HandleFunc("/api/sessions/state", s.handleSessionState)
	s.mux.HandleFunc("/api/traces", s.handleListTraces)
	s.mux.HandleFunc("/api/events/stream", s.Broker.SSEHandler("dashboard"))
	s.mux.HandleFunc("/api/approval/pending", s.Approval.HandlePending)
	s.mux.HandleFunc("/api/approval/respond", s.Approval.HandleRespond)
	s.mux.Handle("/metrics", s.Metrics.Handler())

	// Scheduler API
	s.mux.HandleFunc("/api/schedules", s.handleSchedules)
	s.mux.HandleFunc("/api/schedules/", s.handleScheduleByID)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, `{"status":"ok"}`)
}

func (s *Server) handleLiveness(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, `{"status":"alive"}`)
}

func (s *Server) handleReadiness(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !s.ready.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintln(w, `{"status":"not_ready"}`)
		return
	}
	if err := s.Store.Migrate(context.Background()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, `{"status":"not_ready","error":%q}`+"\n", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, `{"status":"ready"}`)
}

// SetReady marks the server as ready to accept traffic.
func (s *Server) SetReady(ready bool) {
	s.ready.Store(ready)
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
	_ = json.NewEncoder(w).Encode(map[string]any{"sessions": sessions})
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
	_ = json.NewEncoder(w).Encode(map[string]any{"traces": traces})
}

func (s *Server) handleSessionState(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		http.Error(w, `{"error":"session_id query parameter is required"}`, http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		cp, err := s.Store.GetLatestCheckpoint(r.Context(), sessionID)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_id":    sessionID,
			"checkpoint_id": cp.ID,
			"node_id":       cp.NodeID,
			"state":         cp.State,
			"seq_num":       cp.SeqNum,
		})

	case http.MethodPost:
		var body struct {
			State map[string]any `json:"state"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid JSON: %s"}`, err.Error()), http.StatusBadRequest)
			return
		}

		cp, err := s.Store.GetLatestCheckpoint(r.Context(), sessionID)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
			return
		}

		for k, v := range body.State {
			cp.State[k] = v
		}

		cp.ID = fmt.Sprintf("cp_modified_%d", time.Now().UnixNano())
		cp.CreatedAt = time.Now()
		if err := s.Store.SaveCheckpoint(r.Context(), cp); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_id":    sessionID,
			"checkpoint_id": cp.ID,
			"state":         cp.State,
			"message":       "state updated, resume session to continue",
		})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// Start begins serving the control plane with graceful shutdown support.
// It blocks until either the context is canceled or a SIGTERM/SIGINT is received.
func (s *Server) Start(ctx context.Context) error {
	srv := &http.Server{
		Addr:    s.Addr,
		Handler: s.mux,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("ChronosOS starting on %s", s.Addr)
		s.SetReady(true)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	select {
	case <-ctx.Done():
		log.Println("ChronosOS: context canceled, initiating shutdown")
	case sig := <-quit:
		log.Printf("ChronosOS: received signal %s, initiating shutdown", sig)
	case err := <-errCh:
		return err
	}

	s.SetReady(false)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.ShutdownTimeout)
	defer cancel()

	log.Printf("ChronosOS: draining connections (timeout %s)...", s.ShutdownTimeout)
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("ChronosOS: shutdown error: %v", err)
		return fmt.Errorf("shutdown: %w", err)
	}

	if err := s.Store.Close(); err != nil {
		log.Printf("ChronosOS: storage close error: %v", err)
	}

	log.Println("ChronosOS: shutdown complete")
	return nil
}

func (s *Server) handleSchedules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		schedules := s.Scheduler.List()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"schedules": schedules})

	case http.MethodPost:
		var body struct {
			AgentID    string `json:"agent_id"`
			CronExpr   string `json:"cron_expr"`
			Input      string `json:"input"`
			NewSession bool   `json:"new_session"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid JSON: %s"}`, err.Error()), http.StatusBadRequest)
			return
		}
		sched, err := s.Scheduler.Add(body.AgentID, body.CronExpr, body.Input, body.NewSession)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(sched)

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleScheduleByID(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path: /api/schedules/{id} or /api/schedules/{id}/history
	path := strings.TrimPrefix(r.URL.Path, "/api/schedules/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]

	if len(parts) == 2 && parts[1] == "history" {
		history := s.Scheduler.History(id)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"history": history})
		return
	}

	switch r.Method {
	case http.MethodGet:
		sched, err := s.Scheduler.Get(id)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sched)

	case http.MethodDelete:
		if err := s.Scheduler.Remove(id); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"deleted":true}`)

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}
