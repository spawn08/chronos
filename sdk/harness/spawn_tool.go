package harness

import (
	"context"
	"fmt"
	"strings"

	"github.com/spawn08/chronos/engine/tool"
)

// Attach registers the spawn_subagent tool on the parent agent so it can
// delegate sub-tasks. The service already holds the parent's tool registry, so
// no separate parent argument is needed (avoiding a nil-deref). Call it after
// building the parent, since the service is derived from the built agent:
//
//	parent, _ := agent.New(...).Build()
//	svc, _ := harness.NewSubAgentService(parent)
//	svc.Register(harness.SubAgentSpec{Name: "researcher", SystemPrompt: "..."})
//	harness.Attach(svc, harness.NewInProcessRunner(svc))
//
// A builder method is intentionally not provided: sdk/agent must not import
// sdk/harness (harness already imports agent), so wiring lives here.
func Attach(svc *SubAgentService, runner Runner) {
	svc.tools.Register(NewSpawnSubAgentTool(svc, runner))
}

// depthContextKey carries the current subagent nesting depth so the spawn tool
// can enforce a recursion bound across nested delegations.
type depthContextKey struct{}

func depthFromContext(ctx context.Context) int {
	if d, ok := ctx.Value(depthContextKey{}).(int); ok {
		return d
	}
	return 0
}

func withDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, depthContextKey{}, depth)
}

// SpawnToolName is the name the model uses to delegate a sub-task.
const SpawnToolName = "spawn_subagent"

// NewSpawnSubAgentTool returns the tool a parent agent calls to delegate a
// sub-task to a subagent. The subagent runs in a fresh, isolated conversation
// (via runner) and only its final result is returned to the parent — the
// subagent's intermediate reasoning and tool calls never enter the parent's
// context window. The subagent is either a pre-registered template (selected by
// "agent") or one described dynamically in the call.
func NewSpawnSubAgentTool(svc *SubAgentService, runner Runner) *tool.Definition {
	description := "Delegate a self-contained sub-task to a subagent that works in its own fresh context " +
		"and returns only its final result, keeping your context small. Provide 'task'. " +
		"Optionally name a pre-registered 'agent', or describe a new one with 'system_prompt' (and optional 'tools')."
	if catalog := svc.catalog(); len(catalog) > 0 {
		var b strings.Builder
		b.WriteString(" Registered subagents:")
		for _, spec := range catalog {
			b.WriteString("\n- ")
			b.WriteString(spec.Name)
			if spec.Description != "" {
				b.WriteString(": ")
				b.WriteString(spec.Description)
			}
		}
		description += b.String()
	}

	return &tool.Definition{
		Name:         SpawnToolName,
		Description:  description,
		Permission:   tool.PermAllow,
		ParallelSafe: true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task": map[string]any{
					"type":        "string",
					"description": "The self-contained sub-task for the subagent to complete.",
				},
				"agent": map[string]any{
					"type":        "string",
					"description": "Name of a pre-registered subagent to use. Omit to define one dynamically.",
				},
				"system_prompt": map[string]any{
					"type":        "string",
					"description": "For a dynamic subagent: the role/system prompt defining how it should behave.",
				},
				"tools": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "For a dynamic subagent: which of your tools it may use (by name).",
				},
			},
			"required": []any{"task"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			task, _ := args["task"].(string)
			if task == "" {
				return nil, fmt.Errorf("%s: 'task' must be a non-empty string", SpawnToolName)
			}

			depth := depthFromContext(ctx)
			if depth >= svc.maxDepth {
				return nil, fmt.Errorf("%s: maximum subagent depth (%d) reached", SpawnToolName, svc.maxDepth)
			}

			name, _ := args["agent"].(string)
			spec, err := svc.resolve(name, dynamicSpecFromArgs(args))
			if err != nil {
				return nil, fmt.Errorf("%s: %w", SpawnToolName, err)
			}

			result, err := runner.Run(withDepth(ctx, depth+1), spec, task)
			if err != nil {
				return nil, fmt.Errorf("%s: subagent %q: %w", SpawnToolName, spec.Name, err)
			}
			return map[string]any{"agent": spec.Name, "result": result}, nil
		},
	}
}

// dynamicSpecFromArgs extracts a dynamic subagent spec from the tool arguments.
func dynamicSpecFromArgs(args map[string]any) SubAgentSpec {
	spec := SubAgentSpec{}
	if sp, ok := args["system_prompt"].(string); ok {
		spec.SystemPrompt = sp
	}
	if raw, ok := args["tools"].([]any); ok {
		for _, t := range raw {
			if name, ok := t.(string); ok && name != "" {
				spec.ToolNames = append(spec.ToolNames, name)
			}
		}
	}
	return spec
}
