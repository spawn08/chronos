package builtins

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/sandbox"
)

// NewAutoShellTool creates a shell tool that auto-approves all commands.
// Use for autonomous agents that need unsupervised shell access within a sandbox.
// allowedCommands restricts which commands can run; an empty list means all are allowed.
// timeout controls max execution time (0 = 30s default).
func NewAutoShellTool(allowedCommands []string, timeout time.Duration) *tool.Definition {
	def := NewShellTool(allowedCommands, timeout)
	def.Name = "shell"
	def.Permission = tool.PermAllow
	def.Description = "Execute a shell command and return stdout/stderr. Auto-approved for autonomous agents."
	return def
}

// NewShellTool creates a tool that executes shell commands.
// allowedCommands restricts which commands can run; an empty list means all are allowed.
// timeout controls max execution time (0 = 30s default).
func NewShellTool(allowedCommands []string, timeout time.Duration) *tool.Definition {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	allowed := make(map[string]bool, len(allowedCommands))
	for _, c := range allowedCommands {
		allowed[c] = true
	}

	return &tool.Definition{
		Name:        "shell",
		Description: "Execute a shell command and return stdout/stderr. Use with caution.",
		Permission:  tool.PermRequireApproval,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "The shell command to execute",
				},
			},
			"required": []string{"command"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			command, ok := args["command"].(string)
			if !ok || command == "" {
				return nil, fmt.Errorf("shell: 'command' argument is required")
			}

			if len(allowed) > 0 {
				parts := strings.Fields(command)
				if len(parts) == 0 {
					return nil, fmt.Errorf("shell: empty command")
				}
				if !allowed[parts[0]] {
					return nil, fmt.Errorf("shell: command %q is not in the allowed list", parts[0])
				}
			}

			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			cmd := exec.CommandContext(ctx, "sh", "-c", command)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()
			result := map[string]any{
				"stdout":    stdout.String(),
				"stderr":    stderr.String(),
				"exit_code": 0,
			}
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					result["exit_code"] = exitErr.ExitCode()
				} else {
					return nil, fmt.Errorf("shell: %w", err)
				}
			}
			return result, nil
		},
	}
}

// NewSandboxShellTool creates a shell tool that executes commands inside a Sandbox.
// Commands run in isolation — the agent cannot escape the sandbox boundary.
func NewSandboxShellTool(sb sandbox.Sandbox, timeout time.Duration) *tool.Definition {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &tool.Definition{
		Name:        "shell",
		Description: "Execute a shell command inside the sandbox environment. Returns stdout, stderr, and exit code.",
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "The shell command to execute",
				},
			},
			"required": []string{"command"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			command, ok := args["command"].(string)
			if !ok || command == "" {
				return nil, fmt.Errorf("sandbox_shell: 'command' argument is required")
			}
			result, err := sb.Execute(ctx, "sh", []string{"-c", command}, timeout)
			if err != nil {
				return nil, fmt.Errorf("sandbox_shell: %w", err)
			}
			return map[string]any{
				"stdout":    result.Stdout,
				"stderr":    result.Stderr,
				"exit_code": result.ExitCode,
			}, nil
		},
	}
}
