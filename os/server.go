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
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	httpSwagger "github.com/swaggo/http-swagger/v2"

	"github.com/spawn08/chronos/engine/stream"
	_ "github.com/spawn08/chronos/os/apidocs" // registers the generated OpenAPI spec with the swag registry
	"github.com/spawn08/chronos/os/approval"
	"github.com/spawn08/chronos/os/auth"
	"github.com/spawn08/chronos/os/interop/agui"
	"github.com/spawn08/chronos/os/logging"
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
//
// To feed the Metrics registry from execution, add
// hooks.NewPrometheusHook(s.Metrics) to the hook chain of the agents you run
// (agents live outside the control plane, so this is wired at agent
// construction). The registry is exposed at /metrics regardless.
type Server struct {
	Addr            string
	Store           storage.Storage
	Broker          *stream.Broker
	Auth            *auth.Service
	Trace           *trace.Collector
	Approval        *approval.Service
	Metrics         *metrics.Registry
	Scheduler       scheduler.Runner
	ShutdownTimeout time.Duration
	mux             *http.ServeMux
	ready           atomic.Bool

	// a2aHandler, when set (WithA2A), serves the Agent-to-Agent protocol under
	// /a2a/ behind the auth chain and scoped to the caller's tenant. It is an
	// http.Handler (typically an *a2a.Server) so the control plane does not
	// depend on the sdk layer.
	a2aHandler http.Handler

	// Middleware / hardening configuration. Defaults are set in New and can
	// be overridden with the With... options passed to NewWithOptions.
	authMW           func(http.Handler) http.Handler // nil => no authentication
	corsCfg          middleware.CORSConfig
	rateLimitCfg     middleware.RateLimitConfig
	disableCORS      bool
	disableRateLimit bool
	enableRecovery   bool
	swaggerEnabled   bool            // serve the /swagger UI + OpenAPI spec (default true)
	rbacEnabled      bool            // enforce method-based RBAC on /api/* (requires auth)
	logger           *log.Logger     // nil => request logging disabled
	structuredLogger *logging.Logger // nil => structured JSON logging disabled

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

// WithSwagger enables or disables the Swagger UI and OpenAPI spec endpoints
// (/swagger, /swagger/, /swagger/doc.json). It defaults to enabled. Disable it
// on hardened production control planes where the API schema and interactive
// console should not be exposed anonymously.
func WithSwagger(enabled bool) Option {
	return func(s *Server) { s.swaggerEnabled = enabled }
}

// WithRBAC enforces role-based authorization on /api/* routes when
// authentication is enabled: read requests (GET/HEAD) require the viewer role,
// mutating requests require the user role. Roles come from the authenticated
// principal's claims. It is a no-op when authentication is disabled (there is no
// principal to authorize) and defaults to off.
func WithRBAC(enabled bool) Option {
	return func(s *Server) { s.rbacEnabled = enabled }
}

// WithLogger sets the request logger. A nil logger disables request logging.
func WithLogger(l *log.Logger) Option {
	return func(s *Server) { s.logger = l }
}

// WithStructuredLogging installs structured JSON request logging (with a
// per-request correlation id) as the outermost middleware. A nil logger leaves
// structured logging disabled.
func WithStructuredLogging(l *logging.Logger) Option {
	return func(s *Server) { s.structuredLogger = l }
}

// WithScheduler injects a scheduler implementation. The default is the
// in-process scheduler; pass a store-backed scheduler.StoreScheduler for
// exactly-once firing across replicas.
func WithScheduler(r scheduler.Runner) Option {
	return func(s *Server) {
		if r != nil {
			s.Scheduler = r
		}
	}
}

// WithApproval injects a preconfigured approval service (e.g. store-backed and
// authorized via approval.WithStore/WithAuthorizer). The default is an
// in-memory service.
func WithApproval(svc *approval.Service) Option {
	return func(s *Server) {
		if svc != nil {
			s.Approval = svc
		}
	}
}

// WithA2A serves an Agent-to-Agent (A2A) endpoint under /a2a/, exposing a
// Chronos agent to external A2A clients. The handler is typically an
// *a2a.Server (from sdk/protocol/a2a); it is accepted as an http.Handler so the
// control plane stays independent of the sdk layer. The route sits behind the
// auth middleware chain and every request is scoped to the caller's tenant, so
// a task created by one tenant is invisible (404) to another. Passing nil leaves
// the endpoint unregistered.
func WithA2A(h http.Handler) Option {
	return func(s *Server) { s.a2aHandler = h }
}

// WithRateLimiter selects the rate-limit backend. Passing a store-backed
// middleware.Limiter shares limits across replicas; nil keeps the per-process
// default.
func WithRateLimiter(l middleware.Limiter) Option {
	return func(s *Server) { s.rateLimitCfg.Limiter = l }
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
	return []string{
		"/healthz", "/health", "/health/live", "/health/ready", "/metrics",
		"/swagger", "/swagger/", "/swagger/doc.json", "/swagger/index.html",
	}
}

// swaggerPathPrefix is the mount point for the Swagger UI and its assets.
const swaggerPathPrefix = "/swagger"

// isSwaggerPath reports whether the request targets the Swagger UI, its static
// assets, or the generated OpenAPI JSON. These are served without
// authentication so the docs remain reachable when auth is enabled. Callers
// pass an already-canonicalized path (see cleanRequestPath).
func isSwaggerPath(p string) bool {
	return p == swaggerPathPrefix || strings.HasPrefix(p, swaggerPathPrefix+"/")
}

// cleanRequestPath canonicalizes a request path (resolving "." and ".."
// segments) so an auth-bypass decision cannot be tricked by traversal such as
// /swagger/../api/sessions. path.Clean collapses a trailing slash, so
// "/swagger/" becomes "/swagger", which isSwaggerPath still matches.
func cleanRequestPath(p string) string {
	if p == "" {
		return "/"
	}
	return path.Clean(p)
}

// rbacMiddleware enforces coarse method-based authorization on /api/* routes
// using the authenticated principal's roles: read requests require the viewer
// role, mutating requests require the user role. It assumes an upstream auth
// middleware has already placed UserClaims in the request context.
func (s *Server) rbacMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			required := auth.RoleUser
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				required = auth.RoleViewer
			}
			claims, ok := auth.UserFromContext(r.Context())
			if !ok || claims == nil {
				http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
				return
			}
			if !s.Auth.CheckPermission(claims, required) {
				http.Error(w, fmt.Sprintf(`{"error":"insufficient permissions, requires %s"}`, required), http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// New creates a new ChronosOS server with safe defaults: no authentication,
// CORS and rate limiting enabled, panic recovery enabled, and hardened
// http.Server timeouts. The signature is stable; use NewWithOptions to
// customize behavior.
//
// @title                      Chronos ChronosOS API
// @version                    1.0
// @description                Control plane HTTP API for the Chronos agentic framework: sessions, traces, live event streaming, human-in-the-loop approvals, schedules, health, and metrics.
// @BasePath                   /
// @securityDefinitions.apikey BearerAuth
// @in                         header
// @name                       Authorization
// @description                JWT bearer token. Send as "Authorization: Bearer <token>".
// @securityDefinitions.apikey ApiKeyAuth
// @in                         header
// @name                       X-Api-Key
// @description                Static API key sent in the X-Api-Key header.
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
		swaggerEnabled:    true,
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
		// RBAC (when enabled) sits between auth and the mux so the authenticated
		// principal's claims are already in context when roles are checked.
		var inner http.Handler = s.mux
		if s.rbacEnabled {
			inner = s.rbacMiddleware(s.mux)
		}
		authed := s.authMW(inner)
		// Route Swagger UI/assets/doc.json around the auth middleware so the API
		// docs stay reachable even when authentication is enabled. Exact-match
		// SkipPaths cannot cover the UI's arbitrary asset sub-paths, so we branch
		// on the /swagger prefix here (still inside rate-limit/CORS/recovery).
		//
		// The path is canonicalized with path.Clean before the prefix test so a
		// traversal like /swagger/../api/sessions cannot ride the bypass around
		// auth: it normalizes to /api/sessions and takes the authenticated path.
		// This keeps the guarantee independent of the router's own cleaning.
		h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.swaggerEnabled && isSwaggerPath(cleanRequestPath(r.URL.Path)) {
				s.mux.ServeHTTP(w, r)
				return
			}
			authed.ServeHTTP(w, r)
		})
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
	// Structured JSON logging with a correlation id runs outermost so the id is
	// established (and echoed) before any other middleware or handler.
	if s.structuredLogger != nil {
		h = logging.Middleware(s.structuredLogger)(h)
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
	// Empty default topic = firehose: a client with no ?session/?topic sees all
	// sessions' events (dashboard/monitor); ?session=<id> scopes to one session.
	s.mux.HandleFunc("/api/events/stream", s.handleEventsStream)
	// Standardized AG-UI event stream (translated from the native broker events),
	// served alongside the native stream so AG-UI frontends need no custom glue.
	s.mux.HandleFunc("/api/agui/stream", s.handleAGUIStream)
	s.mux.HandleFunc("/api/approval/pending", s.handleApprovalPending)
	s.mux.HandleFunc("/api/approval/respond", s.handleApprovalRespond)
	s.mux.HandleFunc("/metrics", s.handleMetrics)

	// Agent-to-Agent (A2A) protocol endpoint, when configured via WithA2A. It is
	// tenant-scoped and stays behind the auth chain (not in defaultAuthSkipPaths).
	if s.a2aHandler != nil {
		s.mux.HandleFunc("/a2a/", s.handleA2A)
	}

	// Scheduler API
	s.mux.HandleFunc("/api/schedules", s.handleSchedules)
	s.mux.HandleFunc("/api/schedules/", s.handleScheduleByID)

	// Swagger UI + OpenAPI spec. Served under /swagger/ (index.html, doc.json,
	// and the UI assets). /swagger redirects to /swagger/. Disabled via
	// WithSwagger(false) on hardened deployments.
	if s.swaggerEnabled {
		s.mux.HandleFunc("/swagger", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/swagger/", http.StatusMovedPermanently)
		})
		s.mux.Handle("/swagger/", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))
	}
}

// handleHealth reports overall service health.
//
// @Summary     Health check
// @Description Reports overall service health.
// @Tags        Health
// @Produce     json
// @Success     200 {object} map[string]interface{} "status ok"
// @Router      /health [get]
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, `{"status":"ok"}`)
}

// handleLiveness is the Kubernetes liveness probe.
//
// @Summary     Liveness probe
// @Description Reports whether the process is alive.
// @Tags        Health
// @Produce     json
// @Success     200 {object} map[string]interface{} "status alive"
// @Router      /health/live [get]
func (s *Server) handleLiveness(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, `{"status":"alive"}`)
}

// handleReadiness is a cheap liveness-of-readiness check: it only reports the
// in-memory ready flag. Storage migration is performed once at startup (Start),
// not on every probe.
//
// @Summary     Readiness probe
// @Description Reports whether the server is ready to accept traffic.
// @Tags        Health
// @Produce     json
// @Success     200 {object} map[string]interface{} "status ready"
// @Failure     503 {object} map[string]interface{} "not ready"
// @Router      /health/ready [get]
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

// handleEventsStream streams execution events over Server-Sent Events (SSE).
//
// @Summary     Stream events (SSE)
// @Description Streams execution events as Server-Sent Events. With no query params it is a firehose across all sessions; ?session scopes to one session and ?topic filters by topic.
// @Tags        Events
// @Produce     text/event-stream
// @Param       session query string false "Scope the stream to a single session id"
// @Param       topic   query string false "Filter events by topic"
// @Success     200 {string} string "SSE event stream"
// @Security    BearerAuth
// @Security    ApiKeyAuth
// @Router      /api/events/stream [get]
func (s *Server) handleEventsStream(w http.ResponseWriter, r *http.Request) {
	streaming(s.Broker.SSEHandler(""))(w, r)
}

// handleAGUIStream serves the same run as a standardized AG-UI event stream,
// translated from the broker's native events. Scope it with ?session=<id>.
//
// @Summary     Stream a run as AG-UI events
// @Description Server-Sent Events translated to the AG-UI protocol for compatible frontends.
// @Tags        events
// @Produce     text/event-stream
// @Param       session query string false "Scope the stream to a single session id (AG-UI thread)"
// @Param       run     query string false "Run id to report in RUN_STARTED/RUN_FINISHED"
// @Success     200 {string} string "AG-UI SSE event stream"
// @Security    BearerAuth
// @Security    ApiKeyAuth
// @Router      /api/agui/stream [get]
func (s *Server) handleAGUIStream(w http.ResponseWriter, r *http.Request) {
	streaming(agui.Handler(s.Broker))(w, r)
}

// handleA2A serves the Agent-to-Agent protocol (configured via WithA2A). Every
// request is scoped to the caller's tenant so tasks are isolated across tenants
// (cross-tenant access resolves to 404). The task-stream sub-route is served
// through the streaming wrapper so a long-lived SSE response is not cut off by
// the global write timeout.
//
// @Summary     Agent-to-Agent (A2A) protocol
// @Description Exposes a Chronos agent over the A2A protocol: GET /a2a/agent (card), POST /a2a/tasks (create), GET /a2a/tasks/{id} (status), GET /a2a/tasks/{id}/stream (SSE updates), DELETE /a2a/tasks/{id} (cancel). Tenant-scoped.
// @Tags        interop
// @Success     200 {object} map[string]interface{} "A2A response"
// @Failure     404 {object} map[string]interface{} "unknown or cross-tenant task"
// @Security    BearerAuth
// @Security    ApiKeyAuth
// @Router      /a2a/agent [get]
func (s *Server) handleA2A(w http.ResponseWriter, r *http.Request) {
	r = r.WithContext(s.tenantContext(r))
	if strings.HasSuffix(r.URL.Path, "/stream") {
		streaming(s.a2aHandler.ServeHTTP)(w, r)
		return
	}
	s.a2aHandler.ServeHTTP(w, r)
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

// tenantContext derives the tenant scope from the authenticated principal (JWT
// or API-key claims) and returns a context that scopes every storage operation
// to that tenant. Because the tenant comes from the verified principal and never
// from client-supplied ids, id-addressed reads (sessions, traces, checkpoints)
// for another tenant's objects resolve to not-found, closing the IDOR.
// When no principal is present (authentication disabled) the request operates
// under storage.DefaultTenant, preserving single-tenant behavior.
func (s *Server) tenantContext(r *http.Request) context.Context {
	tenant := storage.DefaultTenant
	if claims, ok := auth.UserFromContext(r.Context()); ok && claims != nil && claims.TenantID != "" {
		tenant = claims.TenantID
	}
	return storage.WithTenant(r.Context(), tenant)
}

// handleListSessions lists persisted sessions for the caller's tenant.
//
// @Summary     List sessions
// @Description Lists persisted sessions, optionally filtered by agent, with pagination.
// @Tags        Sessions
// @Produce     json
// @Param       agent_id query string false "Filter by agent id"
// @Param       limit    query int    false "Max results (default 50)"
// @Param       offset   query int    false "Result offset (default 0)"
// @Success     200 {object} map[string]interface{} "sessions"
// @Failure     500 {object} map[string]interface{} "internal error"
// @Security    BearerAuth
// @Security    ApiKeyAuth
// @Router      /api/sessions [get]
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
	sessions, err := s.Store.ListSessions(s.tenantContext(r), agentID, limit, offset)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"sessions": sessions})
}

// handleListTraces returns the execution traces for a session.
//
// @Summary     List traces
// @Description Returns execution traces (spans) for a given session.
// @Tags        Traces
// @Produce     json
// @Param       session_id query string true "Session id"
// @Success     200 {object} map[string]interface{} "traces"
// @Failure     500 {object} map[string]interface{} "internal error"
// @Security    BearerAuth
// @Security    ApiKeyAuth
// @Router      /api/traces [get]
func (s *Server) handleListTraces(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"traces":[],"error":"session_id query parameter is required"}`)
		return
	}
	traces, err := s.Store.ListTraces(s.tenantContext(r), sessionID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"traces": traces})
}

// handleSessionState reads (GET) or patches (POST) the latest checkpoint state
// for a session.
//
// @Summary     Get or update session state
// @Description GET returns the latest checkpoint state for a session. POST merges the supplied state into the latest checkpoint.
// @Tags        Sessions
// @Accept      json
// @Produce     json
// @Param       session_id query    string                 true  "Session id"
// @Param       body       body     map[string]interface{} false "State patch: {\"state\": {...}} (POST only)"
// @Success     200 {object} map[string]interface{} "session state"
// @Failure     400 {object} map[string]interface{} "bad request"
// @Failure     404 {object} map[string]interface{} "session not found"
// @Failure     500 {object} map[string]interface{} "internal error"
// @Security    BearerAuth
// @Security    ApiKeyAuth
// @Router      /api/sessions/state [get]
// @Router      /api/sessions/state [post]
func (s *Server) handleSessionState(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		http.Error(w, `{"error":"session_id query parameter is required"}`, http.StatusBadRequest)
		return
	}

	ctx := s.tenantContext(r)

	switch r.Method {
	case http.MethodGet:
		cp, err := s.Store.GetLatestCheckpoint(ctx, sessionID)
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

		cp, err := s.Store.GetLatestCheckpoint(ctx, sessionID)
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
		if err := s.Store.SaveCheckpoint(ctx, cp); err != nil {
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

	// Migrate the approval store (no-op for the in-memory default) so a
	// store-backed, restart-durable approval service is ready.
	if err := s.Approval.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate approval: %w", err)
	}

	// Start the scheduler (previously constructed but never started). Both the
	// in-process and store-backed schedulers block in Start, so run it in a
	// goroutine and stop it on shutdown.
	schedCtx, stopSched := context.WithCancel(ctx)
	defer stopSched()
	go s.Scheduler.Start(schedCtx)
	defer s.Scheduler.Stop()

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

// handleMetrics exposes the Prometheus metrics registry.
//
// @Summary     Prometheus metrics
// @Description Exposes the Prometheus metrics registry in text exposition format.
// @Tags        Metrics
// @Produce     plain
// @Success     200 {string} string "metrics exposition"
// @Router      /metrics [get]
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	s.Metrics.Handler().ServeHTTP(w, r)
}

// handleApprovalPending lists pending human-in-the-loop approval requests.
//
// @Summary     List pending approvals
// @Description Lists human-in-the-loop approval requests awaiting a decision.
// @Tags        Approval
// @Produce     json
// @Success     200 {object} map[string]interface{} "pending approvals"
// @Security    BearerAuth
// @Security    ApiKeyAuth
// @Router      /api/approval/pending [get]
func (s *Server) handleApprovalPending(w http.ResponseWriter, r *http.Request) {
	s.Approval.HandlePending(w, r)
}

// handleApprovalRespond records an approve/deny decision for a pending request.
//
// @Summary     Respond to an approval
// @Description Records an approve or deny decision for a pending approval request.
// @Tags        Approval
// @Accept      json
// @Produce     json
// @Param       body body map[string]interface{} true "Approval decision"
// @Success     200 {object} map[string]interface{} "decision recorded"
// @Failure     400 {object} map[string]interface{} "bad request"
// @Security    BearerAuth
// @Security    ApiKeyAuth
// @Router      /api/approval/respond [post]
func (s *Server) handleApprovalRespond(w http.ResponseWriter, r *http.Request) {
	s.Approval.HandleRespond(w, r)
}

// handleSchedules lists (GET) or creates (POST) scheduled agent runs.
//
// @Summary     List or create schedules
// @Description GET lists all schedules. POST creates a new cron schedule for an agent.
// @Tags        Schedules
// @Accept      json
// @Produce     json
// @Param       body body map[string]interface{} false "Schedule spec: {agent_id, cron_expr, input, new_session} (POST only)"
// @Success     200 {object} map[string]interface{} "schedules (GET)"
// @Success     201 {object} map[string]interface{} "created schedule (POST)"
// @Failure     400 {object} map[string]interface{} "bad request"
// @Security    BearerAuth
// @Security    ApiKeyAuth
// @Router      /api/schedules [get]
// @Router      /api/schedules [post]
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

// handleScheduleByID gets or deletes a schedule, or returns its run history.
//
// @Summary     Get, delete, or get history of a schedule
// @Description GET returns a schedule; DELETE removes it; GET on the /history sub-path returns past runs.
// @Tags        Schedules
// @Produce     json
// @Param       id path string true "Schedule id"
// @Success     200 {object} map[string]interface{} "schedule, deletion result, or history"
// @Failure     404 {object} map[string]interface{} "not found"
// @Security    BearerAuth
// @Security    ApiKeyAuth
// @Router      /api/schedules/{id} [get]
// @Router      /api/schedules/{id} [delete]
// @Router      /api/schedules/{id}/history [get]
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
