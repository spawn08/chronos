package builtins

import (
	"context"
	"path/filepath"
	"testing"
)

func TestWorkspaceRootOverrideIsImmutablePerContext(t *testing.T) {
	first := filepath.Join(t.TempDir(), "first")
	second := filepath.Join(t.TempDir(), "second")
	parent := WithWorkspaceRoot(context.Background(), first)
	child := WithWorkspaceRoot(parent, second)

	if got, ok := WorkspaceRootFromContext(parent); !ok || got != first {
		t.Fatalf("parent root = %q, %v; want %q", got, ok, first)
	}
	if got, ok := WorkspaceRootFromContext(child); !ok || got != second {
		t.Fatalf("child root = %q, %v; want %q", got, ok, second)
	}
	if got := WorkspaceRoot(context.Background(), first); got != first {
		t.Fatalf("configured fallback = %q, want %q", got, first)
	}
}
