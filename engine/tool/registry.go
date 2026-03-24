// Package tool provides the tool registry with permissions and approval hooks.
package tool

import (
	"context"
	"fmt"
	"sync"
)

// Permission levels for tool execution.
type Permission string

const (
	PermAllow           Permission = "allow"            // auto-approved
	PermRequireApproval Permission = "require_approval" // needs human approval
	PermDeny            Permission = "deny"             // blocked
)

// Definition describes a callable tool.
type Definition struct {
	Name                 string         `json:"name"`
	Description          string         `json:"description"`
	Parameters           map[string]any `json:"parameters"` // JSON Schema
	Permission           Permission     `json:"permission"`
	RequiresConfirmation bool           `json:"requires_confirmation,omitempty"`
	RequiresUserInput    bool           `json:"requires_user_input,omitempty"`
	Handler              Handler        `json:"-"`
}

// Handler is the function signature for tool execution.
type Handler func(ctx context.Context, args map[string]any) (any, error)

// ApprovalFunc is called when a tool requires human approval.
// It should block until approved/denied and return true if approved.
type ApprovalFunc func(ctx context.Context, toolName string, args map[string]any) (bool, error)

// UserInputFunc is called when a tool needs user input before executing.
// It should block until input is provided and return the input string.
type UserInputFunc func(ctx context.Context, toolName string, prompt string) (string, error)

// Registry manages tool definitions, permissions, and execution.
type Registry struct {
	mu        sync.RWMutex
	tools     map[string]*Definition
	approval  ApprovalFunc
	userInput UserInputFunc
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]*Definition),
	}
}

// SetApprovalHandler sets the function called for tools requiring approval.
func (r *Registry) SetApprovalHandler(fn ApprovalFunc) {
	r.approval = fn
}

// SetUserInputHandler sets the function called for tools requiring user input.
func (r *Registry) SetUserInputHandler(fn UserInputFunc) {
	r.userInput = fn
}

// Register adds a tool definition.
func (r *Registry) Register(def *Definition) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[def.Name] = def
}

// List returns all registered tools.
func (r *Registry) List() []*Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Definition, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// Execute runs a tool by name, enforcing permissions, confirmation, and user input.
func (r *Registry) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	r.mu.RLock()
	def, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("tool %q not found", name)
	}

	switch def.Permission {
	case PermDeny:
		return nil, fmt.Errorf("tool %q is denied", name)
	case PermRequireApproval:
		if r.approval == nil {
			return nil, fmt.Errorf("tool %q requires approval but no handler set", name)
		}
		approved, err := r.approval(ctx, name, args)
		if err != nil {
			return nil, fmt.Errorf("approval for %q: %w", name, err)
		}
		if !approved {
			return nil, fmt.Errorf("tool %q: approval denied", name)
		}
	}

	if def.RequiresConfirmation {
		if r.approval == nil {
			return nil, fmt.Errorf("tool %q requires confirmation but no approval handler set", name)
		}
		confirmed, err := r.approval(ctx, name, args)
		if err != nil {
			return nil, fmt.Errorf("confirmation for %q: %w", name, err)
		}
		if !confirmed {
			return nil, fmt.Errorf("tool %q: confirmation denied", name)
		}
	}

	if def.RequiresUserInput {
		if r.userInput == nil {
			return nil, fmt.Errorf("tool %q requires user input but no handler set", name)
		}
		input, err := r.userInput(ctx, name, def.Description)
		if err != nil {
			return nil, fmt.Errorf("user input for %q: %w", name, err)
		}
		if args == nil {
			args = make(map[string]any)
		}
		args["__user_input__"] = input
	}

	return def.Handler(ctx, args)
}

// Get returns a tool definition by name.
func (r *Registry) Get(name string) (*Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.tools[name]
	return def, ok
}
