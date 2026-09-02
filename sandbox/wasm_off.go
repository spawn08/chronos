//go:build !sandbox_wasm

package sandbox

import (
	"context"
	"fmt"
	"time"
)

// WASMConfig mirrors the real WASMConfig (see wasm.go, built with -tags
// sandbox_wasm) so callers compile identically either way.
type WASMConfig struct {
	WASMPath         string
	MemoryLimitPages uint32
	FSDir            string
}

// WASMSandbox is a stub in the default build; see wasm.go under
// -tags sandbox_wasm for the real wazero-backed implementation.
type WASMSandbox struct{}

func (s *WASMSandbox) Execute(ctx context.Context, command string, args []string, timeout time.Duration) (*Result, error) {
	return nil, fmt.Errorf("sandbox: wasm backend not built (rebuild with -tags sandbox_wasm)")
}

func (s *WASMSandbox) Close() error { return nil }

// NewWASMSandbox is disabled in the default build to keep the wazero runtime
// out of the binary — see chronos-code's ROADMAP.md "binary size" goal.
// Rebuild with -tags sandbox_wasm to enable the WASM sandbox backend.
func NewWASMSandbox(wasmPath string) (*WASMSandbox, error) {
	return nil, fmt.Errorf("sandbox: wasm backend not built (rebuild with -tags sandbox_wasm)")
}

// NewWASMSandboxWithConfig is disabled in the default build; see
// NewWASMSandbox.
func NewWASMSandboxWithConfig(cfg WASMConfig) (*WASMSandbox, error) {
	return nil, fmt.Errorf("sandbox: wasm backend not built (rebuild with -tags sandbox_wasm)")
}
