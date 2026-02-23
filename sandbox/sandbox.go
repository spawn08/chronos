// Package sandbox provides isolation for untrusted tool/skill execution.
package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// Sandbox defines the interface for executing untrusted code.
type Sandbox interface {
	Execute(ctx context.Context, command string, args []string, timeout time.Duration) (*Result, error)
	Close() error
}

// Result captures the output of a sandboxed execution.
type Result struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// ProcessSandbox runs commands in a subprocess with timeouts.
// For production, replace with container-based isolation (gVisor, Firecracker, etc.).
type ProcessSandbox struct {
	WorkDir string
}

func NewProcessSandbox(workDir string) *ProcessSandbox {
	return &ProcessSandbox{WorkDir: workDir}
}

func (s *ProcessSandbox) Execute(ctx context.Context, command string, args []string, timeout time.Duration) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = s.WorkDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := &Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return result, fmt.Errorf("sandbox execute: %w", err)
		}
	}

	return result, nil
}

func (s *ProcessSandbox) Close() error { return nil }
