package builtins

import (
	"context"
	"testing"
	"time"
)

func TestSleepTool_Basic(t *testing.T) {
	tool := NewSleepTool(5 * time.Second)

	start := time.Now()
	result, err := tool.Handler(context.Background(), map[string]any{"seconds": 0.1})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("sleep: %v", err)
	}
	if elapsed < 90*time.Millisecond {
		t.Errorf("slept too short: %v", elapsed)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type: %T", result)
	}
	if m["slept_seconds"].(float64) < 0.09 {
		t.Errorf("slept_seconds = %v", m["slept_seconds"])
	}
}

func TestSleepTool_MaxDuration(t *testing.T) {
	tool := NewSleepTool(100 * time.Millisecond)

	start := time.Now()
	_, err := tool.Handler(context.Background(), map[string]any{"seconds": 10.0})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("sleep: %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("should have been capped: %v", elapsed)
	}
}

func TestSleepTool_NegativeSeconds(t *testing.T) {
	tool := NewSleepTool(0)
	_, err := tool.Handler(context.Background(), map[string]any{"seconds": -1.0})
	if err == nil {
		t.Fatal("expected error for negative seconds")
	}
}

func TestSleepTool_InvalidArg(t *testing.T) {
	tool := NewSleepTool(0)
	_, err := tool.Handler(context.Background(), map[string]any{"seconds": "not a number"})
	if err == nil {
		t.Fatal("expected error for string seconds")
	}
}

func TestSleepTool_ContextCancel(t *testing.T) {
	tool := NewSleepTool(0)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := tool.Handler(ctx, map[string]any{"seconds": 10.0})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestSleepTool_Definition(t *testing.T) {
	tool := NewSleepTool(5 * time.Second)
	if tool.Name != "sleep" {
		t.Errorf("Name = %q", tool.Name)
	}
	if tool.Parameters == nil {
		t.Error("Parameters should not be nil")
	}
}
