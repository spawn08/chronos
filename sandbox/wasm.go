package sandbox

import (
	"context"
	"fmt"
	"time"
)

// WASMSandbox is a placeholder for a future WebAssembly (WASI) isolation backend.
// It is not yet implemented: no WASM runtime (e.g. Wazero or Wasmtime) is wired in.
type WASMSandbox struct {
	wasmPath string
}

// NewWASMSandbox reports that the WASM backend is not implemented.
//
// Per P2-001 the stub backends fail at construction rather than deferring the
// error to execution time, so callers discover the missing capability
// immediately. It always returns a nil sandbox and a non-nil error.
func NewWASMSandbox(wasmPath string) (*WASMSandbox, error) {
	return nil, fmt.Errorf("wasm sandbox: not implemented (no WASM runtime integrated; module %q)", wasmPath)
}

func (s *WASMSandbox) Execute(_ context.Context, command string, _ []string, _ time.Duration) (*Result, error) {
	return nil, fmt.Errorf("wasm sandbox: not implemented (module: %s, command: %s)", s.wasmPath, command)
}

func (s *WASMSandbox) Close() error { return nil }
