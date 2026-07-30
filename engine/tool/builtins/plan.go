package builtins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spawn08/chronos/engine/stream"
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/storage"
)

// TaskStatus is the lifecycle state of a single planned task.
type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"     // not started
	TaskInProgress TaskStatus = "in_progress" // actively being worked on
	TaskCompleted  TaskStatus = "completed"   // finished
)

// Valid reports whether s is one of the recognized task statuses.
func (s TaskStatus) Valid() bool {
	switch s {
	case TaskPending, TaskInProgress, TaskCompleted:
		return true
	default:
		return false
	}
}

// PlanTask is one entry in an agent's task list. Tasks are identified by their
// position in the plan: the model rewrites the whole list on every update, so a
// stable id would only go stale.
type PlanTask struct {
	Content string     `json:"content"`
	Status  TaskStatus `json:"status"`
}

// Plan is a structured, revisable task list the agent maintains across turns so
// it does not lose track of subgoals on long, multi-step tasks. It is persisted
// per session (see PlanStore) so it survives checkpoints and resume.
type Plan struct {
	Tasks     []PlanTask `json:"tasks"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Clone returns a deep copy so callers never share the store's backing slice.
func (p *Plan) Clone() *Plan {
	if p == nil {
		return &Plan{}
	}
	out := &Plan{UpdatedAt: p.UpdatedAt}
	if len(p.Tasks) > 0 {
		out.Tasks = make([]PlanTask, len(p.Tasks))
		copy(out.Tasks, p.Tasks)
	}
	return out
}

// Complete reports whether every task in the plan is completed. An empty plan is
// not considered complete.
func (p *Plan) Complete() bool {
	if p == nil || len(p.Tasks) == 0 {
		return false
	}
	for _, t := range p.Tasks {
		if t.Status != TaskCompleted {
			return false
		}
	}
	return true
}

// Summary renders the plan as a human- and model-readable checklist, e.g.
//
//	[x] 1. gather sources
//	[~] 2. draft report
//	[ ] 3. review
func (p *Plan) Summary() string {
	if p == nil || len(p.Tasks) == 0 {
		return "(no plan)"
	}
	var b strings.Builder
	for i, t := range p.Tasks {
		mark := " "
		switch t.Status {
		case TaskCompleted:
			mark = "x"
		case TaskInProgress:
			mark = "~"
		}
		fmt.Fprintf(&b, "[%s] %d. %s\n", mark, i+1, t.Content)
	}
	return strings.TrimRight(b.String(), "\n")
}

// ErrNoSession is returned by a PlanStore when the context carries no session id.
// Planning is an inherently per-session capability, so both store implementations
// reject a sessionless call identically (rather than one silently succeeding and
// the other failing) — use storage.WithSession, ChatWithSession, or a graph run.
var ErrNoSession = errors.New("plan store: no active session in context (use storage.WithSession)")

// PlanStore persists a single agent's plan, scoped to the session and tenant
// carried by the context (see storage.WithSession / storage.WithTenant). Load
// returns a non-nil empty plan when nothing has been saved yet, so callers never
// need a nil check. Both Load and Save require an active session (ErrNoSession
// otherwise); the two implementations are behaviorally substitutable.
type PlanStore interface {
	Load(ctx context.Context) (*Plan, error)
	Save(ctx context.Context, p *Plan) error
}

// sessionScope returns the session id from ctx, or ErrNoSession when none is set.
func sessionScope(ctx context.Context) (string, error) {
	if sessionID := storage.SessionFromContext(ctx); sessionID != "" {
		return sessionID, nil
	}
	return "", ErrNoSession
}

// InMemoryPlanStore is a process-local PlanStore keyed by (tenant, session). It
// is intended for tests and ephemeral use; it does not survive a process
// restart. Use StoragePlanStore for durable, resume-safe persistence.
type InMemoryPlanStore struct {
	mu    sync.RWMutex
	plans map[string]*Plan
}

// NewInMemoryPlanStore creates an empty in-memory plan store.
func NewInMemoryPlanStore() *InMemoryPlanStore {
	return &InMemoryPlanStore{plans: make(map[string]*Plan)}
}

// key derives the composite (tenant, session) key isolating one session's plan
// from another's, including across tenants.
func (s *InMemoryPlanStore) key(ctx context.Context, sessionID string) string {
	return storage.TenantFromContext(ctx) + "\x00" + sessionID
}

// Load returns a clone of the stored plan for the context's session, or an empty
// plan when none exists.
func (s *InMemoryPlanStore) Load(ctx context.Context) (*Plan, error) {
	sessionID, err := sessionScope(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.plans[s.key(ctx, sessionID)]; ok {
		return p.Clone(), nil
	}
	return &Plan{}, nil
}

// Save stores a clone of p for the context's session.
func (s *InMemoryPlanStore) Save(ctx context.Context, p *Plan) error {
	sessionID, err := sessionScope(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plans[s.key(ctx, sessionID)] = p.Clone()
	return nil
}

// planMetadataKey is the Session.Metadata field under which StoragePlanStore
// keeps the current plan.
const planMetadataKey = "__plan__"

// StoragePlanStore persists the plan durably as a single mutable value in the
// session's Session.Metadata, via the storage.Storage backend. Because the plan
// is part of the session record, it survives process restarts and is restored
// when a worker resumes the session — without touching the runner's append-only
// event ledger or its sequence space. The session must already exist (created by
// the agent/runner run or ChatWithSession); reads and writes are tenant-scoped by
// the underlying adapter.
//
// Save is serialized by an internal mutex so a read-modify-write of the session
// record cannot lose a concurrent update (e.g. two team agents sharing a
// session). Distributed single-writer-per-session is additionally guaranteed by
// the durable queue's leased dequeue.
type StoragePlanStore struct {
	store  storage.Storage
	saveMu sync.Mutex // serializes the GetSession→UpdateSession read-modify-write
}

// NewStoragePlanStore creates a durable, session-metadata-backed plan store.
func NewStoragePlanStore(s storage.Storage) *StoragePlanStore {
	return &StoragePlanStore{store: s}
}

// Load reads the current plan from the session's metadata, returning an empty
// plan when the session has no plan yet.
func (s *StoragePlanStore) Load(ctx context.Context) (*Plan, error) {
	sessionID, err := sessionScope(ctx)
	if err != nil {
		return nil, err
	}
	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("plan store: load session %q: %w", sessionID, err)
	}
	raw, ok := sess.Metadata[planMetadataKey]
	if !ok {
		return &Plan{}, nil
	}
	plan, err := decodePlan(raw)
	if err != nil {
		return nil, fmt.Errorf("plan store: decode plan: %w", err)
	}
	return plan, nil
}

// Save writes p into the session's metadata, preserving the rest of the session
// record. It is atomic with respect to concurrent Saves (see the type doc).
func (s *StoragePlanStore) Save(ctx context.Context, p *Plan) error {
	sessionID, err := sessionScope(ctx)
	if err != nil {
		return err
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()

	sess, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("plan store: load session %q: %w", sessionID, err)
	}
	if sess.Metadata == nil {
		sess.Metadata = make(map[string]any)
	}
	sess.Metadata[planMetadataKey] = p.Clone()
	sess.UpdatedAt = time.Now()
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return fmt.Errorf("plan store: update session %q: %w", sessionID, err)
	}
	return nil
}

// decodePlan converts a stored metadata value back into a Plan. Adapters hand
// the value back as a decoded any (typically map[string]any after a JSON
// round-trip), so a re-marshal is the simplest robust conversion.
func decodePlan(payload any) (*Plan, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var p Plan
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// PlanToolName is the name the model uses to update its plan.
const PlanToolName = "update_plan"

const planToolDescription = `Create and maintain a structured task list (todo plan) for a multi-step task.
Call this tool whenever you start a task, finish a step, or revise your approach.
Always send the COMPLETE list of tasks — it replaces the previous plan.
Keep at most one task "in_progress" at a time; mark a task "completed" as soon as it is done.
Use this to stay on track across long tasks; the plan persists across turns.`

// NewPlanTool returns the planning ("todo") tool bound to store. The model calls
// it with the full task list, which replaces the stored plan; the tool persists
// the plan for the session in context and, when broker is non-nil, publishes a
// stream.EventPlanUpdate so UIs can render progress. broker may be nil.
func NewPlanTool(store PlanStore, broker *stream.Broker) *tool.Definition {
	return &tool.Definition{
		Name:        PlanToolName,
		Description: planToolDescription,
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tasks": map[string]any{
					"type":        "array",
					"description": "The full, ordered task list. Sending an empty array clears the plan.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"content": map[string]any{
								"type":        "string",
								"description": "What the task is.",
							},
							"status": map[string]any{
								"type":        "string",
								"enum":        []any{string(TaskPending), string(TaskInProgress), string(TaskCompleted)},
								"description": "Task status; defaults to pending.",
							},
						},
						"required": []any{"content"},
					},
				},
			},
			"required": []any{"tasks"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			plan, err := parsePlanArgs(args)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", PlanToolName, err)
			}
			if err := store.Save(ctx, plan); err != nil {
				return nil, fmt.Errorf("%s: %w", PlanToolName, err)
			}
			publishPlanUpdate(ctx, broker, plan)
			return map[string]any{
				"plan":     plan.Summary(),
				"tasks":    plan.Tasks,
				"complete": plan.Complete(),
			}, nil
		},
	}
}

// NewPlanToolkit wraps NewPlanTool in a toolkit so it can be added to an agent
// with agent.New(...).AddToolkit(builtins.NewPlanToolkit(store, broker)).
func NewPlanToolkit(store PlanStore, broker *stream.Broker) *tool.Toolkit {
	tk := tool.NewToolkit("planning", "Structured task planning: maintain and revise a todo list across turns.")
	tk.Add(NewPlanTool(store, broker))
	return tk
}

// parsePlanArgs validates and normalizes the tool arguments into a Plan. Tasks
// keep their given order. An unrecognized status is an error so the model gets
// clear feedback rather than a silently mangled plan.
func parsePlanArgs(args map[string]any) (*Plan, error) {
	raw, ok := args["tasks"]
	if !ok {
		return nil, fmt.Errorf("'tasks' argument is required")
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("'tasks' must be an array")
	}
	plan := &Plan{Tasks: make([]PlanTask, 0, len(list)), UpdatedAt: time.Now()}
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("task %d must be an object", i+1)
		}
		content, _ := m["content"].(string)
		content = strings.TrimSpace(content)
		if content == "" {
			return nil, fmt.Errorf("task %d: 'content' must be a non-empty string", i+1)
		}
		status := TaskPending
		if s, ok := m["status"].(string); ok && s != "" {
			status = TaskStatus(s)
			if !status.Valid() {
				return nil, fmt.Errorf("task %d: invalid status %q (want pending, in_progress, or completed)", i+1, s)
			}
		}
		plan.Tasks = append(plan.Tasks, PlanTask{Content: content, Status: status})
	}
	return plan, nil
}

// publishPlanUpdate emits a plan-update event, routed to the session's topic
// when one is set so only that session's subscribers receive it. It is a no-op
// when broker is nil.
func publishPlanUpdate(ctx context.Context, broker *stream.Broker, plan *Plan) {
	if broker == nil {
		return
	}
	evt := stream.Event{Type: stream.EventPlanUpdate, Data: map[string]any{
		"tasks":    plan.Tasks,
		"summary":  plan.Summary(),
		"complete": plan.Complete(),
	}}
	if session := storage.SessionFromContext(ctx); session != "" {
		broker.PublishTopic(session, evt)
	} else {
		broker.Publish(evt)
	}
}
