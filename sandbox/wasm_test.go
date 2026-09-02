//go:build sandbox_wasm

package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// minimalWASM is the smallest valid WebAssembly module: the 4-byte magic
// header "\0asm" followed by the little-endian version 1. It has no functions,
// so instantiating it runs nothing and exits cleanly — enough to exercise the
// sandbox's compile/instantiate/result path without a build toolchain.
var minimalWASM = []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

func writeModule(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mod.wasm")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write module: %v", err)
	}
	return path
}

func TestWASMSandbox_ConstructAndExecute(t *testing.T) {
	sb, err := NewWASMSandbox(writeModule(t, minimalWASM))
	if err != nil {
		t.Fatalf("NewWASMSandbox: %v", err)
	}
	defer sb.Close()

	res, err := sb.Execute(context.Background(), "prog", []string{"arg1"}, 5*time.Second)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
}

func TestWASMSandbox_InvalidModule(t *testing.T) {
	// Bytes that are not valid WebAssembly must be rejected at construction.
	_, err := NewWASMSandbox(writeModule(t, []byte("not a wasm module")))
	if err == nil {
		t.Fatal("expected compile error for invalid module")
	}
}

func TestWASMSandbox_CloseIdempotent(t *testing.T) {
	sb, err := NewWASMSandbox(writeModule(t, minimalWASM))
	if err != nil {
		t.Fatalf("NewWASMSandbox: %v", err)
	}
	if err := sb.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// A second Close on an already-closed runtime must not panic.
	_ = sb.Close()
}

func TestWASMSandbox_ConfigRequiresPath(t *testing.T) {
	if _, err := NewWASMSandboxWithConfig(WASMConfig{}); err == nil {
		t.Fatal("expected error when wasm_path is empty")
	}
}

func TestNewFromConfig_WASM_MissingModule(t *testing.T) {
	// A nonexistent module path fails at construction with a read error, so a
	// bad configuration surfaces immediately rather than at execution time.
	_, err := NewFromConfig(Config{Backend: BackendWASM, WASMPath: "/path/to/module.wasm"})
	if err == nil {
		t.Fatal("expected error for missing wasm module")
		return
	}
	if !strings.Contains(err.Error(), "read module") {
		t.Errorf("error = %v, want a read-module error", err)
	}
}

func TestWASMSandbox_NewMissingModule(t *testing.T) {
	// A nonexistent module path fails at construction with a read error whose
	// message names the offending path.
	sb, err := NewWASMSandbox("/path/to/mod.wasm")
	if err == nil {
		t.Fatal("expected read error at construction")
		return
	}
	if sb != nil {
		t.Errorf("expected nil sandbox, got %v", sb)
	}
	if !strings.Contains(err.Error(), "read module") {
		t.Errorf("error = %v, want a read-module error", err)
	}
	if !strings.Contains(err.Error(), "/path/to/mod.wasm") {
		t.Errorf("error should contain module path: %v", err)
	}
}
