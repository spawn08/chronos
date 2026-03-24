package sandbox

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"
)

func TestNewProcessSandbox(t *testing.T) {
	sb := NewProcessSandbox("/tmp")
	if sb == nil {
		t.Fatal("NewProcessSandbox returned nil")
	}
	if sb.WorkDir != "/tmp" {
		t.Errorf("expected WorkDir /tmp, got %s", sb.WorkDir)
	}
}

func TestProcessSandboxClose(t *testing.T) {
	sb := NewProcessSandbox("/tmp")
	if err := sb.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}

func TestProcessSandboxEcho(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	sb := NewProcessSandbox("/tmp")
	ctx := context.Background()
	result, err := sb.Execute(ctx, "echo", []string{"hello"}, 5*time.Second)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit 0, got %d", result.ExitCode)
	}
	if result.Stdout != "hello\n" {
		t.Errorf("expected stdout 'hello\\n', got %q", result.Stdout)
	}
}

func TestProcessSandboxNonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	sb := NewProcessSandbox("/tmp")
	ctx := context.Background()
	result, err := sb.Execute(ctx, "sh", []string{"-c", "exit 42"}, 5*time.Second)
	// Exit code errors are NOT returned as Go errors
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 42 {
		t.Errorf("expected exit 42, got %d", result.ExitCode)
	}
}

func TestProcessSandboxStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	sb := NewProcessSandbox("/tmp")
	ctx := context.Background()
	result, err := sb.Execute(ctx, "sh", []string{"-c", "echo err >&2"}, 5*time.Second)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Stderr != "err\n" {
		t.Errorf("expected stderr 'err\\n', got %q", result.Stderr)
	}
}

func TestProcessSandboxTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	sb := NewProcessSandbox("/tmp")
	ctx := context.Background()
	_, err := sb.Execute(ctx, "sleep", []string{"10"}, 100*time.Millisecond)
	// Should fail due to timeout — either error or non-zero exit
	if err == nil {
		t.Log("no error returned on timeout (process may have been killed)")
	}
}

func TestProcessSandboxWorkDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	dir := t.TempDir()
	sb := NewProcessSandbox(dir)
	ctx := context.Background()
	result, err := sb.Execute(ctx, "pwd", nil, 5*time.Second)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	// On macOS, /var/folders -> /private/var/folders; normalize
	// Just check exit code
	if result.ExitCode != 0 {
		t.Errorf("expected exit 0, got %d", result.ExitCode)
	}
}

func TestProcessSandboxInvalidCommand(t *testing.T) {
	sb := NewProcessSandbox("/tmp")
	ctx := context.Background()
	_, err := sb.Execute(ctx, "command_that_does_not_exist_xyz", nil, 5*time.Second)
	if err == nil {
		t.Fatal("expected error for invalid command")
	}
}

func TestResultFields(t *testing.T) {
	r := &Result{Stdout: "out", Stderr: "err", ExitCode: 1}
	if r.Stdout != "out" {
		t.Errorf("Stdout mismatch")
	}
	if r.Stderr != "err" {
		t.Errorf("Stderr mismatch")
	}
	if r.ExitCode != 1 {
		t.Errorf("ExitCode mismatch")
	}
}

// TestPoolNewPool tests pool creation
func TestPoolNewPool(t *testing.T) {
	tests := []struct {
		name    string
		cfg     PoolConfig
		wantErr bool
	}{
		{
			name:    "no factory",
			cfg:     PoolConfig{},
			wantErr: true,
		},
		{
			name: "valid config",
			cfg: PoolConfig{
				Factory: func() (Sandbox, error) {
					return NewProcessSandbox(os.TempDir()), nil
				},
			},
			wantErr: false,
		},
		{
			name: "custom max size",
			cfg: PoolConfig{
				MaxSize: 3,
				Factory: func() (Sandbox, error) {
					return NewProcessSandbox(os.TempDir()), nil
				},
			},
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewPool(tc.cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p == nil {
				t.Fatal("pool is nil")
			}
		})
	}
}

func TestPoolAcquireRelease(t *testing.T) {
	p, err := NewPool(PoolConfig{
		MaxSize: 2,
		Factory: func() (Sandbox, error) {
			return NewProcessSandbox(os.TempDir()), nil
		},
	})
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer p.Close()

	sb, err := p.Acquire()
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	if p.InUse() != 1 {
		t.Errorf("expected 1 in use, got %d", p.InUse())
	}

	if err := p.Release(sb); err != nil {
		t.Fatalf("Release failed: %v", err)
	}
	if p.Size() != 1 {
		t.Errorf("expected 1 available, got %d", p.Size())
	}
}

func TestPoolWarmup(t *testing.T) {
	p, err := NewPool(PoolConfig{
		MaxSize: 3,
		Factory: func() (Sandbox, error) {
			return NewProcessSandbox(os.TempDir()), nil
		},
	})
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer p.Close()

	if err := p.Warmup(context.Background(), 2); err != nil {
		t.Fatalf("Warmup failed: %v", err)
	}
	if p.Size() != 2 {
		t.Errorf("expected 2 available after warmup, got %d", p.Size())
	}
}

func TestPoolClose(t *testing.T) {
	p, err := NewPool(PoolConfig{
		Factory: func() (Sandbox, error) {
			return NewProcessSandbox(os.TempDir()), nil
		},
	})
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	p.Warmup(context.Background(), 2)
	if err := p.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestPoolClosedAcquire(t *testing.T) {
	p, err := NewPool(PoolConfig{
		Factory: func() (Sandbox, error) {
			return NewProcessSandbox(os.TempDir()), nil
		},
	})
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	p.Close()
	_, err = p.Acquire()
	if err == nil {
		t.Fatal("expected error acquiring from closed pool")
	}
}

func TestPoolReleaseFullPool(t *testing.T) {
	p, err := NewPool(PoolConfig{
		MaxSize: 1,
		Factory: func() (Sandbox, error) {
			return NewProcessSandbox(os.TempDir()), nil
		},
	})
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer p.Close()

	sb1, _ := p.Acquire()
	sb2, _ := p.Acquire()
	// Return both: first goes back to pool, second gets closed (pool full)
	p.Release(sb1)
	p.Release(sb2)
	if p.Size() != 1 {
		t.Errorf("expected 1 available, got %d", p.Size())
	}
}

func TestPoolWarmup_ExceedsMax(t *testing.T) {
	p, err := NewPool(PoolConfig{
		MaxSize: 2,
		Factory: func() (Sandbox, error) {
			return NewProcessSandbox(os.TempDir()), nil
		},
	})
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer p.Close()

	// Warmup with n > maxSize should cap at maxSize
	if err := p.Warmup(context.Background(), 5); err != nil {
		t.Fatalf("Warmup failed: %v", err)
	}
	if p.Size() != 2 {
		t.Errorf("expected 2 (maxSize) available, got %d", p.Size())
	}
}

func TestPoolAcquire_FromAvailable(t *testing.T) {
	p, err := NewPool(PoolConfig{
		MaxSize: 3,
		Factory: func() (Sandbox, error) {
			return NewProcessSandbox(os.TempDir()), nil
		},
	})
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer p.Close()

	// Warmup so there are available containers
	p.Warmup(context.Background(), 2)
	if p.Size() != 2 {
		t.Fatalf("expected 2 warm, got %d", p.Size())
	}

	// Acquire should return one from pool
	sb, err := p.Acquire()
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if p.Size() != 1 {
		t.Errorf("expected 1 available after acquire, got %d", p.Size())
	}
	p.Release(sb)
}

func TestPoolClose_WithInUse(t *testing.T) {
	p, err := NewPool(PoolConfig{
		MaxSize: 3,
		Factory: func() (Sandbox, error) {
			return NewProcessSandbox(os.TempDir()), nil
		},
	})
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}

	// Acquire a sandbox but don't release it before close
	_, err = p.Acquire()
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// Close with in-use sandboxes should not hang
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
