//go:build sandbox_wasm

package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

// defaultWASMMemoryPages caps a module's linear memory. Each page is 64 KiB, so
// 4096 pages is 256 MiB — enough for typical workloads while preventing a module
// from exhausting host memory.
const defaultWASMMemoryPages = 4096

// WASMSandbox runs untrusted WASI (WebAssembly System Interface) modules using
// the pure-Go wazero runtime. WebAssembly gives strong isolation for free: a
// module executes in a linear memory space it cannot escape, and it reaches the
// host only through the capabilities the sandbox explicitly grants. By default
// no filesystem, network, environment, or clock access is provided, so a module
// can compute and read/write its own stdio and nothing more.
//
// The module is compiled once at construction (so an invalid module is rejected
// immediately) and instantiated fresh for each Execute call, keeping executions
// isolated from one another.
type WASMSandbox struct {
	wasmPath string
	runtime  wazero.Runtime
	compiled wazero.CompiledModule
	fsDir    string
}

// WASMConfig holds optional configuration for a WASM sandbox.
type WASMConfig struct {
	// WASMPath is the filesystem path to the .wasm module to run. Required.
	WASMPath string
	// MemoryLimitPages caps the module's linear memory in 64 KiB pages. Zero
	// applies defaultWASMMemoryPages.
	MemoryLimitPages uint32
	// FSDir, when set, is mounted read-write at the module's root ("/"). Empty
	// grants the module no filesystem access at all.
	FSDir string
}

// NewWASMSandbox reads and compiles the module at wasmPath with default limits.
// It returns an error if the file cannot be read or is not a valid WebAssembly
// module, so callers discover a bad module at construction rather than at
// execution time.
func NewWASMSandbox(wasmPath string) (*WASMSandbox, error) {
	return NewWASMSandboxWithConfig(WASMConfig{WASMPath: wasmPath})
}

// NewWASMSandboxWithConfig creates a WASM sandbox from an explicit configuration.
func NewWASMSandboxWithConfig(cfg WASMConfig) (*WASMSandbox, error) {
	if cfg.WASMPath == "" {
		return nil, fmt.Errorf("wasm sandbox: wasm_path is required")
	}
	wasmBytes, err := os.ReadFile(cfg.WASMPath)
	if err != nil {
		return nil, fmt.Errorf("wasm sandbox: read module %q: %w", cfg.WASMPath, err)
	}

	pages := cfg.MemoryLimitPages
	if pages == 0 {
		pages = defaultWASMMemoryPages
	}

	// A background context is used to build the long-lived runtime; per-call
	// contexts scope the actual executions in Execute.
	ctx := context.Background()
	rtCfg := wazero.NewRuntimeConfig().WithMemoryLimitPages(pages).WithCloseOnContextDone(true)
	runtime := wazero.NewRuntimeWithConfig(ctx, rtCfg)

	// WASI preview 1 supplies the syscalls a compiled program expects (argv,
	// stdio, exit). Without it most modules fail to instantiate.
	if _, err = wasi_snapshot_preview1.Instantiate(ctx, runtime); err != nil {
		_ = runtime.Close(ctx)
		return nil, fmt.Errorf("wasm sandbox: instantiate WASI: %w", err)
	}

	compiled, err := runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		_ = runtime.Close(ctx)
		return nil, fmt.Errorf("wasm sandbox: compile module %q: %w", cfg.WASMPath, err)
	}

	return &WASMSandbox{
		wasmPath: cfg.WASMPath,
		runtime:  runtime,
		compiled: compiled,
		fsDir:    cfg.FSDir,
	}, nil
}

// Execute instantiates the compiled module and runs it to completion. command
// becomes argv[0] and args the remaining arguments visible to the module via
// WASI. The timeout bounds the run: when it elapses the module is interrupted
// and its partial stdout/stderr returned. A module that exits non-zero yields a
// Result with the matching ExitCode and a nil error; only host-side failures
// (instantiation, interruption) return an error.
func (s *WASMSandbox) Execute(ctx context.Context, command string, args []string, timeout time.Duration) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	modCfg := wazero.NewModuleConfig().
		WithStdout(&stdout).
		WithStderr(&stderr).
		WithArgs(append([]string{command}, args...)...).
		// Anonymous module name: instances do not collide across concurrent runs.
		WithName("")

	if s.fsDir != "" {
		modCfg = modCfg.WithFSConfig(wazero.NewFSConfig().WithDirMount(s.fsDir, "/"))
	}

	mod, err := s.runtime.InstantiateModule(ctx, s.compiled, modCfg)
	if err != nil {
		// A clean WASI exit surfaces as *sys.ExitError, not a real failure.
		var exitErr *sys.ExitError
		if errors.As(err, &exitErr) {
			return &Result{
				Stdout:   stdout.String(),
				Stderr:   stderr.String(),
				ExitCode: int(exitErr.ExitCode()),
			}, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return &Result{Stdout: stdout.String(), Stderr: stderr.String()},
				fmt.Errorf("wasm sandbox: execution interrupted: %w", ctxErr)
		}
		return nil, fmt.Errorf("wasm sandbox: instantiate %q: %w", s.wasmPath, err)
	}
	// Instantiation returning without an ExitError means the module ran its
	// start/_start function and returned normally (exit code 0).
	_ = mod.Close(ctx)

	return &Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}, nil
}

// Close releases the wazero runtime and all instantiated modules.
func (s *WASMSandbox) Close() error {
	if s.runtime == nil {
		return nil
	}
	if err := s.runtime.Close(context.Background()); err != nil {
		return fmt.Errorf("wasm sandbox: close runtime: %w", err)
	}
	return nil
}
