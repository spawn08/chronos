package graph

import (
	"context"
	"fmt"
	"testing"
)

func TestRegisterEntrypoint_Basic(t *testing.T) {
	fn := func(ctx context.Context, input any) (any, error) {
		n := input.(int)
		return n * 2, nil
	}

	compiled, err := RegisterEntrypoint("double", fn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if compiled.ID != "double" {
		t.Errorf("ID = %q, want double", compiled.ID)
	}
	if compiled.Entry != "double" {
		t.Errorf("Entry = %q, want double", compiled.Entry)
	}
}

func TestRegisterEntrypoint_EmptyName(t *testing.T) {
	_, err := RegisterEntrypoint("", func(ctx context.Context, input any) (any, error) { return nil, nil })
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestRegisterEntrypoint_NilFunc(t *testing.T) {
	_, err := RegisterEntrypoint("test", nil)
	if err == nil {
		t.Fatal("expected error for nil function")
	}
}

func TestRegisterEntrypoint_Execute(t *testing.T) {
	fn := func(ctx context.Context, input any) (any, error) {
		return fmt.Sprintf("hello %v", input), nil
	}

	compiled, err := RegisterEntrypoint("greet", fn)
	if err != nil {
		t.Fatal(err)
	}

	// Execute the node directly
	node := compiled.Nodes["greet"]
	state := State{"input": "world"}
	result, err := node.Fn(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if result["output"] != "hello world" {
		t.Errorf("output = %v, want 'hello world'", result["output"])
	}
}

func TestRegisterTask_Basic(t *testing.T) {
	callCount := 0
	fn := func(ctx context.Context, input any) (any, error) {
		callCount++
		return "result", nil
	}

	taskFn := RegisterTask("my_task", fn)
	state := State{"input": "test"}

	// First call — should execute
	result, err := taskFn(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if result["output"] != "result" {
		t.Errorf("output = %v, want 'result'", result["output"])
	}
	if callCount != 1 {
		t.Errorf("call count = %d, want 1", callCount)
	}

	// Second call with same state (has cache) — should use cache
	result2, err := taskFn(context.Background(), result)
	if err != nil {
		t.Fatal(err)
	}
	if result2["output"] != "result" {
		t.Errorf("output = %v, want 'result'", result2["output"])
	}
	if callCount != 1 {
		t.Errorf("call count = %d, want 1 (cached)", callCount)
	}
}

func TestRegisterTask_NilFunc(t *testing.T) {
	taskFn := RegisterTask("nil_task", nil)
	_, err := taskFn(context.Background(), State{})
	if err == nil {
		t.Fatal("expected error for nil function")
	}
}

func TestRegisterTask_Error(t *testing.T) {
	fn := func(ctx context.Context, input any) (any, error) {
		return nil, fmt.Errorf("task failed")
	}

	taskFn := RegisterTask("fail_task", fn)
	_, err := taskFn(context.Background(), State{"input": "x"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTaskGraph_Basic(t *testing.T) {
	tasks := map[string]TaskFunc{
		"step1": func(ctx context.Context, input any) (any, error) {
			return "step1_done", nil
		},
	}

	compiled, err := TaskGraph("pipeline", tasks)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if compiled.ID != "pipeline" {
		t.Errorf("ID = %q, want pipeline", compiled.ID)
	}
}

func TestTaskGraph_Empty(t *testing.T) {
	_, err := TaskGraph("empty", map[string]TaskFunc{})
	if err == nil {
		t.Fatal("expected error for empty task graph")
	}
}
