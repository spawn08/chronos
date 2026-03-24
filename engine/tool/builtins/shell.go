package builtins

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/spawn08/chronos/engine/tool"
)

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
