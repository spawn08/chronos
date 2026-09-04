// Package tool provides the tool registry with permissions and approval hooks.
package tool

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Permission levels for tool execution.
type Permission string

const (
	PermAllow           Permission = "allow"            // auto-approved
	PermRequireApproval Permission = "require_approval" // needs human approval
	PermDeny            Permission = "deny"             // blocked
)

// PermissionMode controls how approval-gated tools are handled by a registry.
// Explicitly denied tools are never bypassed, including in auto-approve mode.
type PermissionMode string

const (
	PermissionModePrompt      PermissionMode = "prompt"
	PermissionModeAutoApprove PermissionMode = "auto_approve"
	PermissionModeDeny        PermissionMode = "deny"
)

// ParsePermissionMode normalizes CLI/YAML aliases and rejects unknown modes.
func ParsePermissionMode(value string) (PermissionMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "default", "prompt", "ask":
		return PermissionModePrompt, nil
	case "auto_approve", "auto-approve", "bypass":
		return PermissionModeAutoApprove, nil
	case "deny":
		return PermissionModeDeny, nil
	default:
		return "", fmt.Errorf("unknown permission mode %q (want prompt, auto_approve, or deny)", value)
	}
}

// Definition describes a callable tool.
type Definition struct {
	Name                 string         `json:"name"`
	Description          string         `json:"description"`
	Parameters           map[string]any `json:"parameters"` // JSON Schema
	Permission           Permission     `json:"permission"`
	RequiresConfirmation bool           `json:"requires_confirmation,omitempty"`
	RequiresUserInput    bool           `json:"requires_user_input,omitempty"`
	ParallelSafe         bool           `json:"parallel_safe,omitempty"`
	Handler              Handler        `json:"-"`
}

// Handler is the function signature for tool execution.
type Handler func(ctx context.Context, args map[string]any) (any, error)

// ApprovalFunc is called when a tool requires human approval.
// It should block until approved/denied and return true if approved.
type ApprovalFunc func(ctx context.Context, toolName string, args map[string]any) (bool, error)

// Approver is the integration seam a human-in-the-loop approval service (e.g.
// os/approval) plugs into. An implementation blocks until a tool call is
// approved or denied — honoring ctx cancellation — and returns true if approved.
// It is intentionally identical in shape to ApprovalFunc so the control-plane
// approval service can be wired into the tool path without an adapter.
type Approver interface {
	RequestApproval(ctx context.Context, toolName string, args map[string]any) (bool, error)
}

// UserInputFunc is called when a tool needs user input before executing.
// It should block until input is provided and return the input string.
type UserInputFunc func(ctx context.Context, toolName string, prompt string) (string, error)

// Registry manages tool definitions, permissions, and execution.
type Registry struct {
	mu             sync.RWMutex
	tools          map[string]*Definition
	approval       ApprovalFunc
	userInput      UserInputFunc
	permissionMode PermissionMode
}

// NewRegistry creates a new tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools:          make(map[string]*Definition),
		permissionMode: PermissionModePrompt,
	}
}

// SetPermissionMode configures registry-wide handling for tools that require
// approval. Auto-approve skips approval and confirmation prompts but still
// respects PermDeny. Deny rejects every approval-gated tool without prompting.
func (r *Registry) SetPermissionMode(mode PermissionMode) error {
	normalized, err := ParsePermissionMode(string(mode))
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.permissionMode = normalized
	return nil
}

// PermissionMode returns the registry's current approval handling mode.
func (r *Registry) PermissionMode() PermissionMode {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.permissionMode
}

// SetApprovalHandler sets the function called for tools requiring approval.
func (r *Registry) SetApprovalHandler(fn ApprovalFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.approval = fn
}

// SetUserInputHandler sets the function called for tools requiring user input.
func (r *Registry) SetUserInputHandler(fn UserInputFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.userInput = fn
}

// SetApprover installs an Approver as the approval handler for tools whose
// Permission is PermRequireApproval (and for RequiresConfirmation tools). It is
// a convenience over SetApprovalHandler for the common case of connecting the
// control-plane approval service (os/approval) into the tool execution path. A
// nil approver clears any installed handler.
func (r *Registry) SetApprover(a Approver) {
	if a == nil {
		r.SetApprovalHandler(nil)
		return
	}
	r.SetApprovalHandler(a.RequestApproval)
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
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Execute runs a tool by name, enforcing permissions, confirmation, and user input.
func (r *Registry) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	r.mu.RLock()
	def, ok := r.tools[name]
	approval := r.approval
	userInput := r.userInput
	permissionMode := r.permissionMode
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("tool %q not found", name)
	}

	switch def.Permission {
	case PermDeny:
		return nil, fmt.Errorf("tool %q is denied", name)
	case PermRequireApproval:
		switch permissionMode {
		case PermissionModeAutoApprove:
			// Explicit local/operator override. PermDeny was already enforced.
		case PermissionModeDeny:
			return nil, fmt.Errorf("tool %q requires approval but permission mode is deny", name)
		default:
			if approval == nil {
				return nil, fmt.Errorf("tool %q requires approval but no handler set", name)
			}
			approved, err := approval(ctx, name, args)
			if err != nil {
				return nil, fmt.Errorf("approval for %q: %w", name, err)
			}
			if !approved {
				return nil, fmt.Errorf("tool %q: approval denied", name)
			}
		}
	}

	// A permission approval also satisfies confirmation for this call, avoiding
	// two identical prompts for definitions that set both fields.
	needsConfirmation := def.RequiresConfirmation && def.Permission != PermRequireApproval
	if needsConfirmation {
		switch permissionMode {
		case PermissionModeAutoApprove:
			// Explicit local/operator override.
		case PermissionModeDeny:
			return nil, fmt.Errorf("tool %q requires confirmation but permission mode is deny", name)
		default:
			if approval == nil {
				return nil, fmt.Errorf("tool %q requires confirmation but no approval handler set", name)
			}
			confirmed, err := approval(ctx, name, args)
			if err != nil {
				return nil, fmt.Errorf("confirmation for %q: %w", name, err)
			}
			if !confirmed {
				return nil, fmt.Errorf("tool %q: confirmation denied", name)
			}
		}
	}

	if def.RequiresUserInput {
		if userInput == nil {
			return nil, fmt.Errorf("tool %q requires user input but no handler set", name)
		}
		input, err := userInput(ctx, name, def.Description)
		if err != nil {
			return nil, fmt.Errorf("user input for %q: %w", name, err)
		}
		if args == nil {
			args = make(map[string]any)
		}
		args["__user_input__"] = input
	}

	return r.invokeHandler(ctx, def, args)
}

// invokeHandler runs a tool handler, converting any panic into an error so a
// misbehaving tool fails only its own call rather than crashing the process.
func (r *Registry) invokeHandler(ctx context.Context, def *Definition, args map[string]any) (result any, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			result = nil
			err = fmt.Errorf("tool %q panicked: %v", def.Name, rec)
		}
	}()
	return def.Handler(ctx, args)
}

// Get returns a tool definition by name.
func (r *Registry) Get(name string) (*Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.tools[name]
	return def, ok
}
