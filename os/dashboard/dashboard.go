// Package dashboard serves the Chronos visual studio: a read-only API over
// sessions, checkpoints, graph topology, and per-session cost, plus
// resume/time-travel actions that build a fresh graph.Runner against an
// application-supplied GraphRegistry. It is mounted by the ChronosOS control
// plane (os/server.go) behind the same auth/tenant chain as every other
// /api/ route; approvals and live streaming reuse the existing
// /api/approval/* and /api/agui/stream endpoints rather than duplicating
// them here.
package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/hooks"
	"github.com/spawn08/chronos/engine/stream"
	"github.com/spawn08/chronos/storage"
)

// GraphRegistry maps an agent id (storage.Session.AgentID) to the compiled
// graph that agent runs. The dashboard uses it to render a session's graph
// topology and to build the Runner for resume/time-travel requests. Without
// one configured (WithGraphs), those actions return 501.
type GraphRegistry map[string]*graph.CompiledGraph

// Handler serves the dashboard's API. Configure it with the With* methods
// before mounting; it holds no other mutable state.
type Handler struct {
	store  storage.Storage
	broker *stream.Broker
	tracer graph.Tracer
	graphs GraphRegistry
	cost   *hooks.CostTracker
}

// New creates a dashboard Handler over the given store and broker.
func New(store storage.Storage, broker *stream.Broker) *Handler {
	return &Handler{store: store, broker: broker}
}

// WithTracer attaches a tracer so runners built for resume/time-travel record
// spans the same way the original run did.
func (h *Handler) WithTracer(t graph.Tracer) *Handler {
	h.tracer = t
	return h
}

// WithGraphs registers the compiled graphs the dashboard may render and
// resume, keyed by agent id.
func (h *Handler) WithGraphs(g GraphRegistry) *Handler {
	h.graphs = g
	return h
}

// WithCostTracker enriches the dashboard with per-session token/cost
// reporting from an already-wired engine/hooks.CostTracker.
func (h *Handler) WithCostTracker(ct *hooks.CostTracker) *Handler {
	h.cost = ct
	return h
}

// ServeHTTP dispatches on the request path (relative to the mount point —
// callers strip their own prefix, e.g. "/api/dashboard/", before serving).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch strings.Trim(r.URL.Path, "/") {
	case "checkpoints":
		h.handleCheckpoints(w, r)
	case "graph":
		h.handleGraph(w, r)
	case "cost":
		h.handleCost(w, r)
	case "resume":
		h.handleResume(w, r)
	case "timetravel":
		h.handleTimeTravel(w, r)
	default:
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown dashboard endpoint"))
	}
}

// handleCheckpoints lists the full checkpoint history for a session — the
// timeline a developer scrubs through for time-travel. Session listing,
// traces, and live events reuse the existing /api/sessions, /api/traces, and
// /api/agui/stream endpoints; only checkpoint history has no other route.
func (h *Handler) handleCheckpoints(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("session_id query parameter is required"))
		return
	}
	cps, err := h.store.ListCheckpoints(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("list checkpoints: %w", err))
		return
	}
	writeJSON(w, map[string]any{"checkpoints": cps})
}

// handleGraph returns the topology of the graph a session runs, as both a
// renderer-friendly GraphView and Mermaid source.
func (h *Handler) handleGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("session_id query parameter is required"))
		return
	}
	sess, ok := h.requireSession(w, r, sessionID)
	if !ok {
		return
	}
	cg, err := h.graphForSession(sess)
	if err != nil {
		writeError(w, http.StatusNotImplemented, err)
		return
	}
	writeJSON(w, map[string]any{
		"graph_id": cg.ID,
		"view":     cg.ToJSON(),
		"mermaid":  cg.ToMermaid(),
	})
}

// handleCost returns accumulated token usage and cost for a session, when a
// CostTracker has been wired in (WithCostTracker). It requires the session to
// resolve for the caller's tenant first: engine/hooks.CostTracker is a plain
// process-global map keyed only by session id with no tenant awareness of its
// own, so skipping this check would let any authenticated caller read another
// tenant's cost by guessing or knowing its session id.
func (h *Handler) handleCost(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("session_id query parameter is required"))
		return
	}
	if _, ok := h.requireSession(w, r, sessionID); !ok {
		return
	}
	if h.cost == nil {
		writeError(w, http.StatusNotImplemented, fmt.Errorf("dashboard: no cost tracker configured (see WithCostTracker)"))
		return
	}
	report := h.cost.GetSessionCost(sessionID)
	writeJSON(w, report)
}

type resumeRequest struct {
	SessionID string `json:"session_id"`
}

// handleResume continues a paused session from its latest checkpoint —
// equivalent to the sdk/agent Agent.Resume or graph.Runner.Resume path, driven
// from the dashboard instead of application code.
func (h *Handler) handleResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	var body resumeRequest
	if status, err := decodeJSON(r, &body); err != nil {
		writeError(w, status, err)
		return
	}
	if body.SessionID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("session_id is required"))
		return
	}
	sess, ok := h.requireSession(w, r, body.SessionID)
	if !ok {
		return
	}
	cg, err := h.graphForSession(sess)
	if err != nil {
		writeError(w, http.StatusNotImplemented, err)
		return
	}
	rs, err := h.newRunner(cg).Resume(r.Context(), body.SessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("resume: %w", err))
		return
	}
	writeJSON(w, rs)
}

type timeTravelRequest struct {
	CheckpointID string `json:"checkpoint_id"`
}

// handleTimeTravel rewinds a session to a specific checkpoint and re-runs from
// there (graph.Runner.ResumeFromCheckpoint), the visual counterpart to
// PLAN.md's time-travel resume path.
func (h *Handler) handleTimeTravel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, fmt.Errorf("method not allowed"))
		return
	}
	var body timeTravelRequest
	if status, err := decodeJSON(r, &body); err != nil {
		writeError(w, status, err)
		return
	}
	if body.CheckpointID == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("checkpoint_id is required"))
		return
	}
	cp, err := h.store.GetCheckpoint(r.Context(), body.CheckpointID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("checkpoint not found: %w", err))
		return
	}
	sess, ok := h.requireSession(w, r, cp.SessionID)
	if !ok {
		return
	}
	cg, err := h.graphForSession(sess)
	if err != nil {
		writeError(w, http.StatusNotImplemented, err)
		return
	}
	rs, err := h.newRunner(cg).ResumeFromCheckpoint(r.Context(), body.CheckpointID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("time travel: %w", err))
		return
	}
	writeJSON(w, rs)
}

// requireSession loads sessionID scoped to the caller's tenant. On failure it
// writes a 404 (a nonexistent id and another tenant's id are indistinguishable
// by design — see storage.Storage's tenant-scoping contract) and returns
// ok=false; callers should return immediately without writing anything else.
func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request, sessionID string) (*storage.Session, bool) {
	sess, err := h.store.GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("session not found: %w", err))
		return nil, false
	}
	return sess, true
}

// graphForSession looks up the compiled graph registered for sess's agent. It
// requires WithGraphs to have been configured. Session existence/ownership is
// requireSession's job (404), not this function's (501) — keeping the two
// checks separate is what lets callers return the right status for each.
func (h *Handler) graphForSession(sess *storage.Session) (*graph.CompiledGraph, error) {
	if h.graphs == nil {
		return nil, fmt.Errorf("dashboard: no graph registry configured (see WithGraphs)")
	}
	cg, ok := h.graphs[sess.AgentID]
	if !ok {
		return nil, fmt.Errorf("no graph registered for agent %q", sess.AgentID)
	}
	return cg, nil
}

// newRunner builds a fresh Runner for cg. A Runner is single-use, so one is
// constructed per resume/time-travel request rather than held on Handler.
func (h *Handler) newRunner(cg *graph.CompiledGraph) *graph.Runner {
	runner := graph.NewRunner(cg, h.store).WithBroker(h.broker)
	if h.tracer != nil {
		runner = runner.WithTracer(h.tracer)
	}
	return runner
}

// decodeJSON decodes r.Body into dst, distinguishing an oversized body (413 —
// the caller (os/server.go's handleDashboardAPI) already wraps r.Body in
// http.MaxBytesReader) from malformed JSON (400), mirroring os/server.go's
// own decodeJSONBody so a POST to /api/dashboard/* gets the same error
// semantics as every other control-plane endpoint.
func decodeJSON(r *http.Request, dst any) (status int, err error) {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return http.StatusRequestEntityTooLarge, fmt.Errorf("request body too large")
		}
		return http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err)
	}
	return 0, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
