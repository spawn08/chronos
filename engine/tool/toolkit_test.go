package tool

import (
	"context"
	"testing"
)

func TestToolkit_Create(t *testing.T) {
	tk := NewToolkit("math", "Math tools")
	if tk.Name != "math" {
		t.Errorf("Name = %q", tk.Name)
	}
	if !tk.Enabled {
		t.Error("should be enabled by default")
	}
	if tk.Permission != PermAllow {
		t.Errorf("Permission = %q, want PermAllow", tk.Permission)
	}
}

func TestToolkit_AddAndRegister(t *testing.T) {
	tk := NewToolkit("test", "Test toolkit")
	tk.Add(&Definition{
		Name: "tool_a",
		Handler: func(_ context.Context, _ map[string]any) (any, error) {
			return "a", nil
		},
	}).Add(&Definition{
		Name: "tool_b",
		Handler: func(_ context.Context, _ map[string]any) (any, error) {
			return "b", nil
		},
	})

	if len(tk.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tk.Tools))
	}

	registry := NewRegistry()
	tk.Register(registry)

	tools := registry.List()
	if len(tools) != 2 {
		t.Errorf("registry should have 2 tools, got %d", len(tools))
	}
}

func TestToolkit_DisabledSkipsRegistration(t *testing.T) {
	tk := NewToolkit("test", "Test")
	tk.Add(&Definition{Name: "skip_me"})
	tk.Disable()

	registry := NewRegistry()
	tk.Register(registry)

	if len(registry.List()) != 0 {
		t.Error("disabled toolkit should not register tools")
	}
}

func TestToolkit_PermissionInherited(t *testing.T) {
	tk := NewToolkit("dangerous", "Dangerous tools")
	tk.WithPermission(PermRequireApproval)
	tk.Add(&Definition{Name: "risky"})

	if tk.Tools[0].Permission != PermRequireApproval {
		t.Errorf("tool should inherit toolkit permission, got %q", tk.Tools[0].Permission)
	}
}

func TestToolkit_ToolNames(t *testing.T) {
	tk := NewToolkit("test", "Test")
	tk.Add(&Definition{Name: "a"}).Add(&Definition{Name: "b"}).Add(&Definition{Name: "c"})

	names := tk.ToolNames()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "a" || names[1] != "b" || names[2] != "c" {
		t.Errorf("names = %v", names)
	}
}

func TestToolkit_EnableDisable(t *testing.T) {
	tk := NewToolkit("test", "Test")
	tk.Disable()
	if tk.Enabled {
		t.Error("should be disabled")
	}
	tk.Enable()
	if !tk.Enabled {
		t.Error("should be enabled")
	}
}
