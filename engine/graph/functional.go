package graph

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// EntrypointFunc is a function that serves as a graph entrypoint.
type EntrypointFunc func(ctx context.Context, input any) (any, error)

// TaskFunc is a function that serves as a checkpoint-able task.
type TaskFunc func(ctx context.Context, input any) (any, error)

// RegisterEntrypoint wraps a Go function as a graph entrypoint.
// The returned CompiledGraph has a single node that runs the function,
// integrating with checkpointing and durable execution.
func RegisterEntrypoint(name string, fn EntrypointFunc) (*CompiledGraph, error) {
	if name == "" {
		return nil, fmt.Errorf("entrypoint: name is required")
	}
	if fn == nil {
		return nil, fmt.Errorf("entrypoint %q: function is required", name)
	}

	g := New(name)
	g.AddNode(name, func(ctx context.Context, state State) (State, error) {
		input := state["input"]
		result, err := fn(ctx, input)
		if err != nil {
			return state, fmt.Errorf("entrypoint %q: %w", name, err)
		}
		state["output"] = result
		return state, nil
	})
	g.SetEntryPoint(name)
	g.SetFinishPoint(name)

	return g.Compile()
}

// RegisterTask marks a function as a checkpoint-able task. The result is
// automatically cached by task name and input hash. If the task was already
// completed in a previous run (via checkpoint), the cached result is returned
// without re-executing the function.
func RegisterTask(name string, fn TaskFunc) NodeFunc {
	if fn == nil {
		return func(_ context.Context, state State) (State, error) {
			return state, fmt.Errorf("task %q: function is nil", name)
		}
	}

	return func(ctx context.Context, state State) (State, error) {
		cacheKey := taskCacheKey(name, state["input"])

		// Check if cached result exists from a previous checkpoint
		if cached, ok := state[cacheKey]; ok {
			state["output"] = cached
			return state, nil
		}

		input := state["input"]
		result, err := fn(ctx, input)
		if err != nil {
			return state, fmt.Errorf("task %q: %w", name, err)
		}

		// Cache the result in state so checkpointing preserves it
		state[cacheKey] = result
		state["output"] = result
		return state, nil
	}
}

// TaskGraph creates a CompiledGraph from a sequence of named tasks.
// Tasks are chained linearly: the output of each becomes the input of the next.
func TaskGraph(id string, tasks map[string]TaskFunc) (*CompiledGraph, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("task graph %q: at least one task is required", id)
	}

	g := New(id)
	var names []string
	for name := range tasks {
		names = append(names, name)
	}

	for _, name := range names {
		taskFn := RegisterTask(name, tasks[name])
		g.AddNode(name, taskFn)
	}

	// Chain linearly
	g.SetEntryPoint(names[0])
	for i := 0; i < len(names)-1; i++ {
		g.AddEdge(names[i], names[i+1])
	}
	g.SetFinishPoint(names[len(names)-1])

	return g.Compile()
}

// taskCacheKey generates a deterministic cache key from the task name and input.
func taskCacheKey(name string, input any) string {
	data, _ := json.Marshal(input)
	h := sha256.Sum256(append([]byte(name+":"), data...))
	return fmt.Sprintf("__task_cache_%s_%s", name, strings.ToLower(fmt.Sprintf("%x", h[:8])))
}
