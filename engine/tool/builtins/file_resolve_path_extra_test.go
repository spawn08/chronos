package builtins

import (
	"path/filepath"
	"testing"
)

func TestResolvePath_Absolute(t *testing.T) {
	got := resolvePath("/workspace", "/etc/passwd")
	if got != "/etc/passwd" {
		t.Errorf("got %q", got)
	}
}

func TestResolvePath_RelativeWithBase(t *testing.T) {
	base := "/workspace/proj"
	got := resolvePath(base, "src/main.go")
	want := filepath.Join(base, "src/main.go")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolvePath_EmptyBase(t *testing.T) {
	got := resolvePath("", "relative.txt")
	if got != "relative.txt" {
		t.Errorf("got %q", got)
	}
}
