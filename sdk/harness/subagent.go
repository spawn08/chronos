// Package harness provides agent-harness orchestration primitives that compose
// over the SDK agent loop — context-isolated and dynamic subagents, and the
// "deep agent" preset that assembles them with planning (WC-A-001) and the
// virtual filesystem (WC-A-002).
package harness

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/sdk/agent"
)

// DefaultMaxSubAgentDepth bounds how deeply subagents may spawn further
// subagents, guarding against unbounded recursion (an agent that keeps
// delegating to itself).
const DefaultMaxSubAgentDepth = 3

// SubAgentSpec describes a subagent to run. A spec is either pre-registered with
// a SubAgentService (selected by Name at spawn time) or supplied dynamically in
// the spawn_subagent call, so a parent can invent a specialist for a task at
// runtime rather than only at build time.
type SubAgentSpec struct {
	// Name identifies the subagent (used as its agent id and, for registered
	// specs, the key the parent selects it by).
	Name string `json:"name"`
	// Description is a human-readable summary surfaced to the model.
	Description string `json:"description,omitempty"`
	// SystemPrompt is the subagent's system prompt; it defines its role.
	SystemPrompt string `json:"system_prompt,omitempty"`
	// Instructions are additional system-level instructions.
	Instructions []string `json:"instructions,omitempty"`
	// ToolNames selects which of the parent's tools the subagent may use. An
	// empty list gives the subagent no tools (pure reasoning). Names not present
	// in the parent registry are rejected at build time.
	ToolNames []string `json:"tool_names,omitempty"`
}

// SubAgentService builds subagents that inherit the parent's model but run in a
// fresh, isolated conversation. It resolves pre-registered specs by name and
// builds dynamic ones on demand, granting each subagent only an explicit subset
// of the parent's tools.
//
// It is a registry and is safe for concurrent use: Register may be called while
// spawns are in flight, mirroring tool.Registry and skill.Registry. model and
// tools are set once at construction and never mutated.
type SubAgentService struct {
	model model.Provider
	tools *tool.Registry

	mu         sync.RWMutex
	registered map[string]SubAgentSpec
	maxDepth   int
}

// Option configures a SubAgentService.
type Option func(*SubAgentService)

// WithMaxDepth overrides the maximum subagent nesting depth. A value <= 0
// restores DefaultMaxSubAgentDepth.
func WithMaxDepth(n int) Option {
	return func(s *SubAgentService) {
		if n <= 0 {
			n = DefaultMaxSubAgentDepth
		}
		s.maxDepth = n
	}
}

// NewSubAgentService creates a service that builds subagents inheriting parent's
// model and drawing tools from parent's registry. parent must have a model.
func NewSubAgentService(parent *agent.Agent, opts ...Option) (*SubAgentService, error) {
	if parent == nil || parent.Model == nil {
		return nil, fmt.Errorf("harness: subagent service requires a parent agent with a model")
	}
	s := &SubAgentService{
		model:      parent.Model,
		tools:      parent.Tools,
		registered: make(map[string]SubAgentSpec),
		maxDepth:   DefaultMaxSubAgentDepth,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// Register adds a named subagent template the parent can select at spawn time.
func (s *SubAgentService) Register(spec SubAgentSpec) error {
	if spec.Name == "" {
		return fmt.Errorf("harness: subagent spec requires a name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registered[spec.Name] = spec
	return nil
}

// Registered returns the names of all pre-registered subagents, sorted for
// deterministic output.
func (s *SubAgentService) Registered() []string {
	catalog := s.catalog()
	names := make([]string, len(catalog))
	for i, spec := range catalog {
		names[i] = spec.Name
	}
	return names
}

// catalog returns a snapshot of the registered specs sorted by name.
func (s *SubAgentService) catalog() []SubAgentSpec {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SubAgentSpec, 0, len(s.registered))
	for _, spec := range s.registered {
		out = append(out, spec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// lookup returns the registered spec for name, if any.
func (s *SubAgentService) lookup(name string) (SubAgentSpec, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	spec, ok := s.registered[name]
	return spec, ok
}

// build constructs a fresh, isolated agent from spec. The subagent has its own
// system prompt and only the explicitly granted tools; it shares no conversation
// history with the parent.
func (s *SubAgentService) build(spec SubAgentSpec) (*agent.Agent, error) {
	if spec.SystemPrompt == "" && len(spec.Instructions) == 0 {
		return nil, fmt.Errorf("harness: subagent %q needs a system prompt or instructions", spec.Name)
	}
	b := agent.New("subagent:"+spec.Name, spec.Name).
		WithModel(s.model).
		WithSystemPrompt(spec.SystemPrompt)
	for _, inst := range spec.Instructions {
		b.AddInstruction(inst)
	}
	for _, name := range spec.ToolNames {
		def, ok := s.tools.Get(name)
		if !ok {
			return nil, fmt.Errorf("harness: subagent %q requests unknown tool %q", spec.Name, name)
		}
		b.AddTool(def)
	}
	return b.Build()
}

// resolve returns the spec to run for a spawn request. A non-empty name must
// match a registered subagent — it fails closed on an unknown name so a typo
// surfaces as an error rather than silently spawning an ad-hoc agent. An empty
// name selects the dynamic spec built from the request, which must carry a
// system prompt or instructions.
func (s *SubAgentService) resolve(name string, dynamic SubAgentSpec) (SubAgentSpec, error) {
	if name != "" {
		spec, ok := s.lookup(name)
		if !ok {
			return SubAgentSpec{}, fmt.Errorf("harness: unknown registered subagent %q", name)
		}
		return spec, nil
	}
	if dynamic.Name == "" {
		dynamic.Name = "dynamic"
	}
	if dynamic.SystemPrompt == "" && len(dynamic.Instructions) == 0 {
		return SubAgentSpec{}, fmt.Errorf("harness: dynamic subagent needs a system_prompt or instructions")
	}
	return dynamic, nil
}

// run builds spec and executes it on task in a fresh conversation, returning only
// the final result. It is the single execution path shared by InProcessRunner
// and the durable subagent graph node, so both behave identically.
func (s *SubAgentService) run(ctx context.Context, spec SubAgentSpec, task string) (string, error) {
	sub, err := s.build(spec)
	if err != nil {
		return "", fmt.Errorf("harness: build subagent %q: %w", spec.Name, err)
	}
	result, err := sub.Execute(ctx, task)
	if err != nil {
		return "", fmt.Errorf("harness: subagent %q: %w", spec.Name, err)
	}
	return result, nil
}

// Runner executes a subagent described by spec on a task and returns only its
// final result. Each strategy runs the subagent where it belongs: InProcessRunner
// runs it in the caller's process; the durable QueuedRunner enqueues it so a
// worker runs it, resumable across worker restarts. Both return just the result
// string, so the subagent's intermediate tokens never reach the parent's context.
type Runner interface {
	Run(ctx context.Context, spec SubAgentSpec, task string) (string, error)
}

// InProcessRunner runs the subagent directly in the caller's process via the
// standard agent loop. It is the default and the only option for dynamic
// subagents (whose definition cannot be reconstructed on another worker).
type InProcessRunner struct {
	svc *SubAgentService
}

// NewInProcessRunner creates an in-process runner over svc.
func NewInProcessRunner(svc *SubAgentService) *InProcessRunner {
	return &InProcessRunner{svc: svc}
}

// Run builds spec via the service and executes it on task in a fresh conversation.
func (r *InProcessRunner) Run(ctx context.Context, spec SubAgentSpec, task string) (string, error) {
	return r.svc.run(ctx, spec, task)
}
