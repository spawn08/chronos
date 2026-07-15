// Package approval provides human-in-the-loop approval workflows for the
// ChronosOS control plane.
//
// The Service is:
//   - ctx-aware: RequestApproval honors caller cancellation/deadline instead of
//     blocking forever;
//   - persisted (optionally): with a Store, pending requests and their decisions
//     survive a process restart and are visible across replicas, so a decision
//     recorded on one node resolves a waiter on another;
//   - authorized (optionally): an Authorizer gates who may resolve a request.
//
// The Service satisfies engine/tool.Approver (RequestApproval has the matching
// signature), so it can be wired into the tool execution path via
// Registry.SetApprover for tools whose Permission is PermRequireApproval.
package approval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Request states.
const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusDenied   = "denied"
)

// ErrNotFound is returned when an approval request does not exist.
var ErrNotFound = errors.New("approval: request not found")

// Request represents an approval request for a tool call.
type Request struct {
	ID        string         `json:"id"`
	ToolName  string         `json:"tool_name"`
	Args      map[string]any `json:"args"`
	Status    string         `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
	// Response is the in-process wakeup channel for a live waiter. It is never
	// persisted or serialized.
	Response chan bool `json:"-"`
}

// Store persists approval requests so they survive restarts and are shared
// across replicas. All timestamps are passed explicitly for deterministic tests.
type Store interface {
	// Migrate creates the approval schema (idempotent).
	Migrate(ctx context.Context) error
	// Create records a new pending request.
	Create(ctx context.Context, req *Request) error
	// Resolve transitions a pending request to approved/denied and returns the
	// updated request. It returns ErrNotFound if the request does not exist.
	Resolve(ctx context.Context, id string, approved bool, now time.Time) (*Request, error)
	// Get returns a request by ID (ErrNotFound if absent).
	Get(ctx context.Context, id string) (*Request, error)
	// List returns all pending requests.
	List(ctx context.Context) ([]*Request, error)
	// Close releases resources owned by the store.
	Close() error
}

// Authorizer decides whether the HTTP caller responding to an approval request
// is permitted to resolve it. Returning a non-nil error rejects the response
// with 403. A nil Authorizer (the default) permits all responders.
type Authorizer interface {
	Authorize(r *http.Request, req *Request) error
}

// Service manages pending approval requests.
type Service struct {
	mu       sync.Mutex
	pending  map[string]*Request // in-process live waiters
	counter  uint64
	store    Store         // optional; nil => in-memory only
	authz    Authorizer    // optional; nil => allow all responders
	pollFreq time.Duration // store poll cadence for cross-replica resolution
	now      func() time.Time
}

// Option configures a Service.
type Option func(*Service)

// WithStore backs the Service with a persistent Store. Pending requests and
// decisions then survive restarts and are shared across replicas.
func WithStore(s Store) Option { return func(sv *Service) { sv.store = s } }

// WithAuthorizer installs an Authorizer that gates approval responses.
func WithAuthorizer(a Authorizer) Option { return func(sv *Service) { sv.authz = a } }

// WithPollInterval sets how often a store-backed waiter polls for a decision
// recorded elsewhere (another replica or after a restart). Ignored without a
// Store. Default is 1s.
func WithPollInterval(d time.Duration) Option {
	return func(sv *Service) {
		if d > 0 {
			sv.pollFreq = d
		}
	}
}

// NewService creates an approval Service. With no options it is a ctx-aware,
// in-memory service.
func NewService(opts ...Option) *Service {
	s := &Service{
		pending:  make(map[string]*Request),
		pollFreq: time.Second,
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Migrate initializes the backing store schema, if any.
func (s *Service) Migrate(ctx context.Context) error {
	if s.store == nil {
		return nil
	}
	if err := s.store.Migrate(ctx); err != nil {
		return fmt.Errorf("approval: migrate: %w", err)
	}
	return nil
}

func (s *Service) nextID(toolName string) string {
	n := atomic.AddUint64(&s.counter, 1)
	return fmt.Sprintf("approval_%s_%d_%d", toolName, s.now().UnixNano(), n)
}

func (s *Service) removePending(id string) {
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()
}

// RequestApproval submits a tool call for human approval and blocks until it is
// resolved or ctx is done. It returns whether the call was approved. The
// signature matches engine/tool.Approver so the Service can be wired into the
// tool path via Registry.SetApprover.
func (s *Service) RequestApproval(ctx context.Context, toolName string, args map[string]any) (bool, error) {
	req := &Request{
		ID:        s.nextID(toolName),
		ToolName:  toolName,
		Args:      args,
		Status:    StatusPending,
		CreatedAt: s.now(),
		Response:  make(chan bool, 1),
	}

	s.mu.Lock()
	s.pending[req.ID] = req
	s.mu.Unlock()
	defer s.removePending(req.ID)

	if s.store != nil {
		if err := s.store.Create(ctx, req); err != nil {
			return false, fmt.Errorf("approval: persist request: %w", err)
		}
	}

	// A store-backed waiter also polls, so a decision recorded on another replica
	// or after a restart still resolves this waiter (the in-process channel only
	// covers same-process responses).
	var pollC <-chan time.Time
	if s.store != nil {
		t := time.NewTicker(s.pollFreq)
		defer t.Stop()
		pollC = t.C
	}

	for {
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("approval: wait canceled: %w", ctx.Err())
		case approved := <-req.Response:
			return approved, nil
		case <-pollC:
			r, err := s.store.Get(ctx, req.ID)
			if err != nil {
				// Transient read error: keep waiting rather than fail the tool.
				continue
			}
			switch r.Status {
			case StatusApproved:
				return true, nil
			case StatusDenied:
				return false, nil
			}
		}
	}
}

// lookup returns the current request, preferring the persistent store.
func (s *Service) lookup(ctx context.Context, id string) (*Request, error) {
	if s.store != nil {
		return s.store.Get(ctx, id)
	}
	s.mu.Lock()
	req, ok := s.pending[id]
	s.mu.Unlock()
	if !ok {
		return nil, ErrNotFound
	}
	return req, nil
}

// resolve records a decision (persisting it if store-backed) and wakes any live
// in-process waiter. It returns ErrNotFound if the request is unknown.
func (s *Service) resolve(ctx context.Context, id string, approved bool) error {
	if s.store != nil {
		if _, err := s.store.Resolve(ctx, id, approved, s.now()); err != nil {
			return err
		}
	}

	s.mu.Lock()
	req, ok := s.pending[id]
	s.mu.Unlock()
	if ok {
		// Buffered channel of size 1; non-blocking send avoids a stuck responder
		// if the waiter already departed (e.g. ctx canceled).
		select {
		case req.Response <- approved:
		default:
		}
		return nil
	}

	// Not a live local waiter. With a store the decision is still durably
	// recorded (handled above); without one, an unknown id is a 404.
	if s.store == nil {
		return ErrNotFound
	}
	return nil
}

// HandlePending returns all pending approval requests.
func (s *Service) HandlePending(w http.ResponseWriter, r *http.Request) {
	var reqs []*Request
	if s.store != nil {
		list, err := s.store.List(r.Context())
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
			return
		}
		reqs = list
	} else {
		s.mu.Lock()
		reqs = make([]*Request, 0, len(s.pending))
		for _, req := range s.pending {
			reqs = append(reqs, req)
		}
		s.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"pending": reqs})
}

// HandleRespond processes an approval response. It authorizes the responder (if
// an Authorizer is configured) and resolves the request.
func (s *Service) HandleRespond(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID       string `json:"id"`
		Approved bool   `json:"approved"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if s.authz != nil {
		req, err := s.lookup(r.Context(), body.ID)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err := s.authz.Authorize(r, req); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusForbidden)
			return
		}
	}

	switch err := s.resolve(r.Context(), body.ID, body.Approved); {
	case err == nil:
		w.WriteHeader(http.StatusOK)
	case errors.Is(err, ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	default:
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
	}
}
