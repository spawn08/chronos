package tool

import (
	"context"
	"errors"
	"testing"
)

func TestRegistry_Get_Found(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:    "mytool",
		Handler: func(_ context.Context, _ map[string]any) (any, error) { return "ok", nil },
	})

	def, ok := r.Get("mytool")
	if !ok {
		t.Fatal("expected tool to be found")
	}
	if def.Name != "mytool" {
		t.Errorf("Name=%q, want mytool", def.Name)
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("expected ok=false for nonexistent tool")
	}
}

func TestRegistry_RequiresConfirmation_Approved(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:                 "confirm_tool",
		RequiresConfirmation: true,
		Handler: func(_ context.Context, _ map[string]any) (any, error) {
			return "confirmed-result", nil
		},
	})
	r.SetApprovalHandler(func(_ context.Context, _ string, _ map[string]any) (bool, error) {
		return true, nil
	})

	result, err := r.Execute(context.Background(), "confirm_tool", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "confirmed-result" {
		t.Errorf("result=%v, want confirmed-result", result)
	}
}

func TestRegistry_RequiresConfirmation_Denied(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:                 "confirm_tool",
		RequiresConfirmation: true,
		Handler:              func(_ context.Context, _ map[string]any) (any, error) { return "ok", nil },
	})
	r.SetApprovalHandler(func(_ context.Context, _ string, _ map[string]any) (bool, error) {
		return false, nil
	})

	_, err := r.Execute(context.Background(), "confirm_tool", nil)
	if err == nil {
		t.Fatal("expected error when confirmation denied")
	}
}

func TestRegistry_RequiresConfirmation_NoHandler(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:                 "confirm_tool",
		RequiresConfirmation: true,
		Handler:              func(_ context.Context, _ map[string]any) (any, error) { return "ok", nil },
	})

	_, err := r.Execute(context.Background(), "confirm_tool", nil)
	if err == nil {
		t.Fatal("expected error when no confirmation handler set")
	}
}

func TestRegistry_RequiresUserInput_Success(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:              "input_tool",
		Description:       "prompt user",
		RequiresUserInput: true,
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			return args["__user_input__"], nil
		},
	})
	r.SetUserInputHandler(func(_ context.Context, _ string, _ string) (string, error) {
		return "user provided value", nil
	})

	result, err := r.Execute(context.Background(), "input_tool", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result != "user provided value" {
		t.Errorf("result=%v, want 'user provided value'", result)
	}
}

func TestRegistry_RequiresUserInput_NoHandler(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:              "input_tool",
		RequiresUserInput: true,
		Handler:           func(_ context.Context, _ map[string]any) (any, error) { return "ok", nil },
	})

	_, err := r.Execute(context.Background(), "input_tool", nil)
	if err == nil {
		t.Fatal("expected error when no user input handler set")
	}
}

func TestRegistry_RequiresUserInput_HandlerError(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:              "input_tool",
		RequiresUserInput: true,
		Handler:           func(_ context.Context, _ map[string]any) (any, error) { return "ok", nil },
	})
	r.SetUserInputHandler(func(_ context.Context, _ string, _ string) (string, error) {
		return "", errors.New("user canceled")
	})

	_, err := r.Execute(context.Background(), "input_tool", nil)
	if err == nil {
		t.Fatal("expected error from user input handler failure")
	}
}

func TestRegistry_RequiresUserInput_ExistingArgs(t *testing.T) {
	// Ensure user input is added to existing args map
	r := NewRegistry()
	r.Register(&Definition{
		Name:              "input_tool",
		RequiresUserInput: true,
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			return args, nil
		},
	})
	r.SetUserInputHandler(func(_ context.Context, _ string, _ string) (string, error) {
		return "typed input", nil
	})

	result, err := r.Execute(context.Background(), "input_tool", map[string]any{"existing": "value"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	args, _ := result.(map[string]any)
	if args["existing"] != "value" {
		t.Errorf("expected existing arg preserved, got %v", args)
	}
	if args["__user_input__"] != "typed input" {
		t.Errorf("expected __user_input__ set, got %v", args["__user_input__"])
	}
}

func TestRegistry_SetUserInputHandler(t *testing.T) {
	r := NewRegistry()
	called := false
	r.SetUserInputHandler(func(_ context.Context, _ string, _ string) (string, error) {
		called = true
		return "input", nil
	})
	if r.userInput == nil {
		t.Error("userInput handler should be set")
	}
	r.userInput(context.Background(), "tool", "prompt")
	if !called {
		t.Error("handler should have been called")
	}
}

func TestRegistry_ConcurrentRegisterAndExecute(t *testing.T) {
	r := NewRegistry()

	// Register tools concurrently
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		name := string(rune('a' + i))
		go func(n string) {
			r.Register(&Definition{
				Name:    n,
				Handler: func(_ context.Context, _ map[string]any) (any, error) { return n, nil },
			})
		}(name)
	}
	close(done)

	// Execute should not panic
	r.Register(&Definition{
		Name:    "safe",
		Handler: func(_ context.Context, _ map[string]any) (any, error) { return "safe", nil },
	})
	_, err := r.Execute(context.Background(), "safe", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestRegistry_ListWithPermissions(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{Name: "allowed", Permission: PermAllow, Handler: func(_ context.Context, _ map[string]any) (any, error) { return nil, nil }})
	r.Register(&Definition{Name: "denied", Permission: PermDeny, Handler: func(_ context.Context, _ map[string]any) (any, error) { return nil, nil }})
	r.Register(&Definition{Name: "approval", Permission: PermRequireApproval, Handler: func(_ context.Context, _ map[string]any) (any, error) { return nil, nil }})

	tools := r.List()
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}
	perms := make(map[Permission]bool)
	for _, t := range tools {
		perms[t.Permission] = true
	}
	if !perms[PermAllow] || !perms[PermDeny] || !perms[PermRequireApproval] {
		t.Errorf("expected all permission types in list: %v", perms)
	}
}
