package tool

import (
	"context"
	"errors"
	"testing"
)

func TestRegistry_Get(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{Name: "x", Handler: func(_ context.Context, _ map[string]any) (any, error) { return 1, nil }})

	def, ok := r.Get("x")
	if !ok || def.Name != "x" {
		t.Fatalf("Get: ok=%v name=%v", ok, def)
	}
	_, ok = r.Get("missing")
	if ok {
		t.Fatal("expected false for missing tool")
	}
}

func TestExecute_RequiresConfirmation_NoHandler(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:                 "c",
		RequiresConfirmation: true,
		Handler:              func(_ context.Context, _ map[string]any) (any, error) { return nil, nil },
	})
	_, err := r.Execute(context.Background(), "c", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExecute_RequiresConfirmation_Denied(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:                 "c",
		RequiresConfirmation: true,
		Handler:              func(_ context.Context, _ map[string]any) (any, error) { return "no", nil },
	})
	r.SetApprovalHandler(func(_ context.Context, _ string, _ map[string]any) (bool, error) {
		return false, nil
	})
	_, err := r.Execute(context.Background(), "c", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExecute_RequiresConfirmation_Error(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:                 "c",
		RequiresConfirmation: true,
		Handler:              func(_ context.Context, _ map[string]any) (any, error) { return nil, nil },
	})
	r.SetApprovalHandler(func(_ context.Context, _ string, _ map[string]any) (bool, error) {
		return false, errors.New("boom")
	})
	_, err := r.Execute(context.Background(), "c", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExecute_RequiresUserInput_NoHandler(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:              "u",
		RequiresUserInput: true,
		Description:       "prompt",
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			return args["__user_input__"], nil
		},
	})
	_, err := r.Execute(context.Background(), "u", map[string]any{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExecute_RequiresUserInput_HandlerError(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:              "u",
		RequiresUserInput: true,
		Handler:           func(_ context.Context, _ map[string]any) (any, error) { return nil, nil },
	})
	r.SetUserInputHandler(func(_ context.Context, _ string, _ string) (string, error) {
		return "", errors.New("input failed")
	})
	_, err := r.Execute(context.Background(), "u", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExecute_RequiresUserInput_NilArgs(t *testing.T) {
	r := NewRegistry()
	r.Register(&Definition{
		Name:              "u",
		RequiresUserInput: true,
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			return args["__user_input__"], nil
		},
	})
	r.SetUserInputHandler(func(_ context.Context, _ string, _ string) (string, error) {
		return "typed", nil
	})
	v, err := r.Execute(context.Background(), "u", nil)
	if err != nil {
		t.Fatal(err)
	}
	if v != "typed" {
		t.Errorf("got %v", v)
	}
}

func TestToolkit_Add_InheritsPermissionWhenToolEmpty(t *testing.T) {
	tk := NewToolkit("t", "d")
	tk.WithPermission(PermDeny)
	tk.Add(&Definition{Name: "n", Permission: ""})

	if tk.Tools[0].Permission != PermDeny {
		t.Errorf("Permission = %q", tk.Tools[0].Permission)
	}
}

func TestToolkit_Register_DisabledNoop(t *testing.T) {
	tk := NewToolkit("t", "d")
	tk.Add(&Definition{Name: "only", Handler: func(_ context.Context, _ map[string]any) (any, error) { return 1, nil }})
	tk.Disable()
	r := NewRegistry()
	tk.Register(r)
	if len(r.List()) != 0 {
		t.Error("disabled toolkit should register nothing")
	}
}
