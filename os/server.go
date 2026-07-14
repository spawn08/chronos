// Package chronosos provides the ChronosOS control plane server.
package chronosos

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/spawn08/chronos/os/middleware"
	"github.com/spawn08/chronos/os/scheduler"
	"github.com/spawn08/chronos/os/trace"
	"github.com/spawn08/chronos/storage"
)

// Default server hardening values. These are applied by New and can be
// overridden with the corresponding With... options.
const (
	defaultReadTimeout       = 15 * time.Second
	defaultReadHeaderTimeout = 10 * time.Second
	defaultWriteTimeout      = 30 * time.Second
	defaultIdleTimeout       = 120 * time.Second
	defaultMaxHeaderBytes    = 1 << 20 // 1 MiB
	defaultMaxBodyBytes      = 1 << 20 // 1 MiB
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

	// Middleware / hardening configuration. Defaults are set in New and can
	// be overridden with the With... options passed to NewWithOptions.
	authMW           func(http.Handler) http.Handler // nil => no authentication
	corsCfg          middleware.CORSConfig
	rateLimitCfg     middleware.RateLimitConfig
	disableCORS      bool
	disableRateLimit bool
	enableRecovery   bool
	logger           *log.Logger // nil => request logging disabled

	readTimeout       time.Duration
	readHeaderTimeout time.Duration
	writeTimeout      time.Duration
	idleTimeout       time.Duration
	maxHeaderBytes    int
	maxBodyBytes      int64
}

// Option configures a Server during construction. Options are applied by
// NewWithOptions before routes are registered.
type Option func(*Server)

// WithAuthMiddleware installs a custom authentication middleware. Passing a
// nil middleware disables authentication (the default).
func WithAuthMiddleware(mw func(http.Handler) http.Handler) Option {
	return func(s *Server) { s.authMW = mw }
}

// WithJWTAuth enables JWT bearer-token authentication using the given config.
// Health and metrics endpoints are always exempt.
func WithJWTAuth(cfg auth.JWTConfig) Option {
	return func(s *Server) {
		cfg.SkipPaths = append(cfg.SkipPaths, defaultAuthSkipPaths()...)
		s.authMW = auth.JWTMiddleware(cfg)
	}
}

// WithAPIKeyAuth enables API-key authentication using the given config.
// Health and metrics endpoints are always exempt.
func WithAPIKeyAuth(cfg auth.APIKeyConfig) Option {
	return func(s *Server) {
		cfg.SkipPaths = append(cfg.SkipPaths, defaultAuthSkipPaths()...)
		s.authMW = auth.APIKeyMiddleware(cfg)
	}
}

// WithCORS overrides the CORS configuration.
func WithCORS(cfg middleware.CORSConfig) Option {
	return func(s *Server) { s.corsCfg = cfg }
}

// WithoutCORS disables the CORS middleware.
func WithoutCORS() Option {
	return func(s *Server) { s.disableCORS = true }
}

// WithRateLimit overrides the rate-limit configuration.
func WithRateLimit(cfg middleware.RateLimitConfig) Option {
	return func(s *Server) { s.rateLimitCfg = cfg }
}

// WithoutRateLimit disables the rate-limit middleware.
func WithoutRateLimit() Option {
	return func(s *Server) { s.disableRateLimit = true }
}

// WithLogger sets the request logger. A nil logger disables request logging.
func WithLogger(l *log.Logger) Option {
	return func(s *Server) { s.logger = l }
}

// WithTimeouts overrides the http.Server timeouts. A zero value for any
// timeout leaves it unbounded (use with care).
func WithTimeouts(read, readHeader, write, idle time.Duration) Option {
	return func(s *Server) {
		s.readTimeout = read
		s.readHeaderTimeout = readHeader
		s.writeTimeout = write
		s.idleTimeout = idle
	}
}

// WithMaxHeaderBytes overrides the maximum request header size.
func WithMaxHeaderBytes(n int) Option {
	return func(s *Server) { s.maxHeaderBytes = n }
}

// WithMaxBodyBytes overrides the maximum JSON request body size enforced via
// http.MaxBytesReader.
func WithMaxBodyBytes(n int64) Option {
	return func(s *Server) { s.maxBodyBytes = n }
}

// defaultAuthSkipPaths lists endpoints that must remain reachable without
// authentication (liveness/readiness probes and metrics scraping).
func defaultAuthSkipPaths() []string {
	return []string{"/healthz", "/health", "/health/live", "/health/ready", "/metrics"}
}

// New creates a new ChronosOS server with safe defaults: no authentication,
// CORS and rate limiting enabled, panic recovery enabled, and hardened
// http.Server timeouts. The signature is stable; use NewWithOptions to
// customize behavior.
func New(addr string, store storage.Storage) *Server {
	return NewWithOptions(addr, store)
}

// NewWithOptions creates a new ChronosOS server, applying the given options
// after defaults are set and before routes are registered.
func NewWithOptions(addr string, store storage.Storage, opts ...Option) *Server {
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
		ShutdownTimeout:   15 * time.Second,
		mux:               http.NewServeMux(),
		corsCfg:           middleware.DefaultCORSConfig(),
		rateLimitCfg:      middleware.DefaultRateLimitConfig(),
		enableRecovery:    true,
		logger:            log.Default(),
		readTimeout:       defaultReadTimeout,
		readHeaderTimeout: defaultReadHeaderTimeout,
		writeTimeout:      defaultWriteTimeout,
		idleTimeout:       defaultIdleTimeout,
		maxHeaderBytes:    defaultMaxHeaderBytes,
		maxBodyBytes:      defaultMaxBodyBytes,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.routes()
	return s
}

// Handler composes the middleware chain around the mux. Requests flow through
// the chain in the order: recovery -> request-logging -> CORS -> rate-limit ->
// auth, then reach the route handlers.
func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux

	// Wrap innermost-first so the outermost wrapper runs first at request time.
	if s.authMW != nil {
		h = s.authMW(h)
	}
	if !s.disableRateLimit {
		h = middleware.RateLimit(s.rateLimitCfg)(h)
	}
	if !s.disableCORS {
		h = middleware.CORS(s.corsCfg)(h)
	}
	if s.logger != nil {
		h = middleware.RequestLogger(s.logger)(h)
	}
	if s.enableRecovery {
		h = middleware.Recovery(s.logger)(h)
	}
	return h
}

// httpServer builds the hardened *http.Server used by Start.
func (s *Server) httpServer() *http.Server {
	return &http.Server{
		Addr:              s.Addr,
		Handler:           s.Handler(),
		ReadTimeout:       s.readTimeout,
		ReadHeaderTimeout: s.readHeaderTimeout,
		WriteTimeout:      s.writeTimeout,
		IdleTimeout:       s.idleTimeout,
		MaxHeaderBytes:    s.maxHeaderBytes,
	}
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
	s.mux.HandleFunc("/api/events/stream", streaming(s.Broker.SSEHandler("dashboard")))
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

// handleReadiness is a cheap liveness-of-readiness check: it only reports the
// in-memory ready flag. Storage migration is performed once at startup (Start),
// not on every probe.
func (s *Server) handleReadiness(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !s.ready.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintln(w, `{"status":"not_ready"}`)
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, `{"status":"ready"}`)
}

// streaming wraps a streaming handler (e.g. SSE) to clear the connection's
// write deadline so a global WriteTimeout does not terminate long-lived
// responses. Non-streaming routes keep the configured WriteTimeout.
func streaming(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		_ = rc.SetWriteDeadline(time.Time{})
		next(w, r)
	}
}

// SetReady marks the server as ready to accept traffic.
func (s *Server) SetReady(ready bool) {
	s.ready.Store(ready)
}

// decodeJSONBody wraps the request body with http.MaxBytesReader and decodes it
// into dst. On failure it returns the HTTP status code to send (413 when the
// body exceeds the configured limit, 400 for malformed JSON) and a descriptive
// error; on success it returns (0, nil).
func (s *Server) decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) (int, error) {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return http.StatusRequestEntityTooLarge, fmt.Errorf("request body too large")
		}
		return http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err)
	}
	return 0, nil
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
		if code, err := s.decodeJSONBody(w, r, &body); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), code)
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

		// Upsert the latest checkpoint in place (same id and seq_num) so the
		// edited state is what a subsequent Resume loads. Minting a new id at the
		// same (session, seq_num) would violate the uq_checkpoints_session_seq
		// index: a hard error on Postgres and a silent row-replace on SQLite.
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
	// Run schema migration once at startup rather than on every readiness probe.
	if err := s.Store.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate storage: %w", err)
	}

	srv := s.httpServer()

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
		if code, err := s.decodeJSONBody(w, r, &body); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), code)
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
