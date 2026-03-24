// Package builtins provides ready-to-use tool implementations.
package builtins

import (
	"context"
	"fmt"
	"time"

	"github.com/spawn08/chronos/engine/tool"
)

// NewSleepTool creates a tool that pauses execution for a specified duration.
// maxDuration limits the maximum sleep time (0 means unlimited).
func NewSleepTool(maxDuration time.Duration) *tool.Definition {
	return &tool.Definition{
		Name:        "sleep",
		Description: "Pause execution for a specified number of seconds. Useful for rate limiting and polling patterns.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"seconds": map[string]any{
					"type":        "number",
					"description": "Number of seconds to sleep (can be fractional)",
				},
			},
			"required": []any{"seconds"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			seconds, ok := args["seconds"].(float64)
			if !ok {
				return nil, fmt.Errorf("sleep: seconds must be a number")
			}
			if seconds < 0 {
				return nil, fmt.Errorf("sleep: seconds must be non-negative")
			}

			dur := time.Duration(seconds * float64(time.Second))
			if maxDuration > 0 && dur > maxDuration {
				dur = maxDuration
			}

			select {
			case <-time.After(dur):
				return map[string]any{"slept_seconds": dur.Seconds()}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
}
