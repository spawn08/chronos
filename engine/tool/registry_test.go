package tool

import (
	"context"
	"errors"
	"testing"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if len(r.List()) != 0 {
		t.Error("new registry should be empty")
	}
}

func TestRegister_And_List(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name: "calc", Description: "calculator",
		Handler: func(_ context.Context, _ map[string]any) (any, error) { return nil, nil },
	})
	r.Register(&Definition{
		Name: "search", Description: "web search",
		Handler: func(_ context.Context, _ map[string]any) (any, error) { return nil, nil },
	})

	tools := r.List()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
}

func TestExecute_Allow(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:       "calc",
		Permission: PermAllow,
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			return args["x"], nil
		},
	})

	result, err := r.Execute(context.Background(), "calc", map[string]any{"x": 42})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != 42 {
		t.Errorf("result = %v, want 42", result)
	}
}

func TestExecute_NotFound(t *testing.T) {
	r := NewRegistry()
	_, err := r.Execute(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for missing tool")
	}
}

func TestExecute_Denied(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:       "dangerous",
		Permission: PermDeny,
		Handler:    func(_ context.Context, _ map[string]any) (any, error) { return nil, nil },
	})

	_, err := r.Execute(context.Background(), "dangerous", nil)
	if err == nil {
		t.Fatal("expected error for denied tool")
	}
}

func TestExecute_RequireApproval_NoHandler(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:       "risky",
		Permission: PermRequireApproval,
		Handler:    func(_ context.Context, _ map[string]any) (any, error) { return nil, nil },
	})

	_, err := r.Execute(context.Background(), "risky", nil)
	if err == nil {
		t.Fatal("expected error when no approval handler set")
	}
}

func TestExecute_RequireApproval_Approved(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:       "risky",
		Permission: PermRequireApproval,
		Handler: func(_ context.Context, _ map[string]any) (any, error) {
			return "executed", nil
		},
	})
	r.SetApprovalHandler(func(_ context.Context, _ string, _ map[string]any) (bool, error) {
		return true, nil
	})

	result, err := r.Execute(context.Background(), "risky", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "executed" {
		t.Errorf("result = %v, want executed", result)
	}
}

func TestExecute_RequireApproval_Denied(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:       "risky",
		Permission: PermRequireApproval,
		Handler:    func(_ context.Context, _ map[string]any) (any, error) { return nil, nil },
	})
	r.SetApprovalHandler(func(_ context.Context, _ string, _ map[string]any) (bool, error) {
		return false, nil
	})

	_, err := r.Execute(context.Background(), "risky", nil)
	if err == nil {
		t.Fatal("expected error when approval denied")
	}
}

func TestExecute_RequireApproval_Error(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:       "risky",
		Permission: PermRequireApproval,
		Handler:    func(_ context.Context, _ map[string]any) (any, error) { return nil, nil },
	})
	r.SetApprovalHandler(func(_ context.Context, _ string, _ map[string]any) (bool, error) {
		return false, errors.New("approval system down")
	})

	_, err := r.Execute(context.Background(), "risky", nil)
	if err == nil {
		t.Fatal("expected error from approval system failure")
	}
}

func TestExecute_HandlerError(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name: "fail",
		Handler: func(_ context.Context, _ map[string]any) (any, error) {
			return nil, errors.New("tool failed")
		},
	})

	_, err := r.Execute(context.Background(), "fail", nil)
	if err == nil {
		t.Fatal("expected error from handler")
	}
}

func TestRegister_Overwrite(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name: "tool", Description: "v1",
		Handler: func(_ context.Context, _ map[string]any) (any, error) { return "v1", nil },
	})
	r.Register(&Definition{
		Name: "tool", Description: "v2",
		Handler: func(_ context.Context, _ map[string]any) (any, error) { return "v2", nil },
	})

	result, err := r.Execute(context.Background(), "tool", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "v2" {
		t.Errorf("result = %v, want v2 (overwritten)", result)
	}
}

func TestExecute_DefaultPermission(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name: "tool",
		Handler: func(_ context.Context, _ map[string]any) (any, error) {
			return "ok", nil
		},
	})

	result, err := r.Execute(context.Background(), "tool", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %v, want ok", result)
	}
}
