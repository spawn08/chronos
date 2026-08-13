package tool

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestPermissionModeAutoApprove(t *testing.T) {
	registry := NewRegistry()
	var approvals atomic.Int32
	registry.SetApprovalHandler(func(context.Context, string, map[string]any) (bool, error) {
		approvals.Add(1)
		return false, nil
	})
	registry.Register(&Definition{
		Name:                 "write",
		Permission:           PermRequireApproval,
		RequiresConfirmation: true,
		Handler: func(context.Context, map[string]any) (any, error) {
			return "ok", nil
		},
	})
	if err := registry.SetPermissionMode(PermissionModeAutoApprove); err != nil {
		t.Fatalf("SetPermissionMode: %v", err)
	}

	got, err := registry.Execute(context.Background(), "write", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got != "ok" {
		t.Fatalf("result = %v, want ok", got)
	}
	if approvals.Load() != 0 {
		t.Fatalf("approval handler called %d times, want 0", approvals.Load())
	}
}

func TestPermissionModeAutoApproveStillRespectsExplicitDeny(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&Definition{
		Name:       "blocked",
		Permission: PermDeny,
		Handler:    func(context.Context, map[string]any) (any, error) { return "unexpected", nil },
	})
	if err := registry.SetPermissionMode(PermissionModeAutoApprove); err != nil {
		t.Fatalf("SetPermissionMode: %v", err)
	}
	if _, err := registry.Execute(context.Background(), "blocked", nil); err == nil {
		t.Fatal("explicitly denied tool executed in auto-approve mode")
	}
}

func TestPermissionApprovalAlsoSatisfiesConfirmation(t *testing.T) {
	registry := NewRegistry()
	var approvals atomic.Int32
	registry.SetApprovalHandler(func(context.Context, string, map[string]any) (bool, error) {
		approvals.Add(1)
		return true, nil
	})
	registry.Register(&Definition{
		Name:                 "database_write",
		Permission:           PermRequireApproval,
		RequiresConfirmation: true,
		Handler:              func(context.Context, map[string]any) (any, error) { return nil, nil },
	})
	if _, err := registry.Execute(context.Background(), "database_write", nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if approvals.Load() != 1 {
		t.Fatalf("approval handler called %d times, want exactly once", approvals.Load())
	}
}

func TestParsePermissionMode(t *testing.T) {
	for input, want := range map[string]PermissionMode{
		"":             PermissionModePrompt,
		"ask":          PermissionModePrompt,
		"bypass":       PermissionModeAutoApprove,
		"auto-approve": PermissionModeAutoApprove,
		"deny":         PermissionModeDeny,
	} {
		got, err := ParsePermissionMode(input)
		if err != nil || got != want {
			t.Errorf("ParsePermissionMode(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := ParsePermissionMode("unsafe-ish"); err == nil {
		t.Fatal("expected unknown mode error")
	}
}
