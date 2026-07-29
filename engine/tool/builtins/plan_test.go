package builtins

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/stream"
	"github.com/spawn08/chronos/storage"
)

func TestParsePlanArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      map[string]any
		wantErr   bool
		wantTasks []PlanTask
	}{
		{
			name: "valid with statuses",
			args: map[string]any{"tasks": []any{
				map[string]any{"content": "gather", "status": "completed"},
				map[string]any{"content": "draft", "status": "in_progress"},
				map[string]any{"content": "review"},
			}},
			wantTasks: []PlanTask{
				{Content: "gather", Status: TaskCompleted},
				{Content: "draft", Status: TaskInProgress},
				{Content: "review", Status: TaskPending},
			},
		},
		{
			name:      "empty list clears plan",
			args:      map[string]any{"tasks": []any{}},
			wantTasks: []PlanTask{},
		},
		{
			name:    "missing tasks",
			args:    map[string]any{},
			wantErr: true,
		},
		{
			name:    "tasks not an array",
			args:    map[string]any{"tasks": "nope"},
			wantErr: true,
		},
		{
			name:    "blank content",
			args:    map[string]any{"tasks": []any{map[string]any{"content": "   "}}},
			wantErr: true,
		},
		{
			name:    "invalid status",
			args:    map[string]any{"tasks": []any{map[string]any{"content": "x", "status": "bogus"}}},
			wantErr: true,
		},
		{
			name:    "task not an object",
			args:    map[string]any{"tasks": []any{"just a string"}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := parsePlanArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(plan.Tasks) != len(tt.wantTasks) {
				t.Fatalf("got %d tasks, want %d", len(plan.Tasks), len(tt.wantTasks))
			}
			for i, want := range tt.wantTasks {
				if plan.Tasks[i] != want {
					t.Errorf("task %d = %+v, want %+v", i, plan.Tasks[i], want)
				}
			}
		})
	}
}

func TestPlanSummaryAndComplete(t *testing.T) {
	tests := []struct {
		name         string
		plan         *Plan
		wantComplete bool
		wantSummary  string
	}{
		{name: "nil", plan: nil, wantComplete: false, wantSummary: "(no plan)"},
		{name: "empty", plan: &Plan{}, wantComplete: false, wantSummary: "(no plan)"},
		{
			name: "mixed",
			plan: &Plan{Tasks: []PlanTask{
				{Content: "a", Status: TaskCompleted},
				{Content: "b", Status: TaskInProgress},
				{Content: "c", Status: TaskPending},
			}},
			wantComplete: false,
			wantSummary:  "[x] 1. a\n[~] 2. b\n[ ] 3. c",
		},
		{
			name: "all done",
			plan: &Plan{Tasks: []PlanTask{
				{Content: "a", Status: TaskCompleted},
				{Content: "b", Status: TaskCompleted},
			}},
			wantComplete: true,
			wantSummary:  "[x] 1. a\n[x] 2. b",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.plan.Complete(); got != tt.wantComplete {
				t.Errorf("Complete() = %v, want %v", got, tt.wantComplete)
			}
			if got := tt.plan.Summary(); got != tt.wantSummary {
				t.Errorf("Summary() = %q, want %q", got, tt.wantSummary)
			}
		})
	}
}

func TestInMemoryPlanStore_SessionAndTenantIsolation(t *testing.T) {
	store := NewInMemoryPlanStore()
	base := context.Background()

	// Two sessions under the default tenant.
	ctxS1 := storage.WithSession(base, "s1")
	ctxS2 := storage.WithSession(base, "s2")
	// Same session id under a different tenant must not collide.
	ctxT2 := storage.WithTenant(storage.WithSession(base, "s1"), "tenant-b")

	if err := store.Save(ctxS1, &Plan{Tasks: []PlanTask{{Content: "one", Status: TaskPending}}}); err != nil {
		t.Fatalf("save s1: %v", err)
	}
	if err := store.Save(ctxT2, &Plan{Tasks: []PlanTask{{Content: "tenant-b", Status: TaskPending}}}); err != nil {
		t.Fatalf("save tenant-b: %v", err)
	}

	p1, _ := store.Load(ctxS1)
	if len(p1.Tasks) != 1 || p1.Tasks[0].Content != "one" {
		t.Errorf("s1 plan = %+v, want [one]", p1.Tasks)
	}
	p2, _ := store.Load(ctxS2)
	if len(p2.Tasks) != 0 {
		t.Errorf("s2 plan should be empty, got %+v", p2.Tasks)
	}
	pt, _ := store.Load(ctxT2)
	if len(pt.Tasks) != 1 || pt.Tasks[0].Content != "tenant-b" {
		t.Errorf("tenant-b plan = %+v, want [tenant-b]", pt.Tasks)
	}
}

func TestInMemoryPlanStore_LoadReturnsClone(t *testing.T) {
	store := NewInMemoryPlanStore()
	ctx := storage.WithSession(context.Background(), "s")
	if err := store.Save(ctx, &Plan{Tasks: []PlanTask{{Content: "orig", Status: TaskPending}}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, _ := store.Load(ctx)
	loaded.Tasks[0].Content = "mutated"
	again, _ := store.Load(ctx)
	if again.Tasks[0].Content != "orig" {
		t.Errorf("store was mutated through a returned plan: %q", again.Tasks[0].Content)
	}
}

// Both PlanStore implementations reject a sessionless context identically, so a
// caller can swap them without behavior change (LSP).
func TestPlanStore_SessionlessRejectedByBothImpls(t *testing.T) {
	ctx := context.Background() // no session
	stores := map[string]PlanStore{
		"in_memory": NewInMemoryPlanStore(),
		"storage":   NewStoragePlanStore(newDurableStore(t)),
	}
	for name, store := range stores {
		t.Run(name, func(t *testing.T) {
			if err := store.Save(ctx, &Plan{}); !errors.Is(err, ErrNoSession) {
				t.Errorf("Save error = %v, want ErrNoSession", err)
			}
			if _, err := store.Load(ctx); !errors.Is(err, ErrNoSession) {
				t.Errorf("Load error = %v, want ErrNoSession", err)
			}
		})
	}
}

func TestPlanTool_HandlerPersistsAndEmits(t *testing.T) {
	store := NewInMemoryPlanStore()
	broker := stream.NewBroker()
	defer broker.Close()
	sub := broker.Subscribe("test-sub")
	defer broker.Unsubscribe("test-sub")

	def := NewPlanTool(store, broker)
	if def.Name != PlanToolName {
		t.Fatalf("tool name = %q, want %q", def.Name, PlanToolName)
	}

	ctx := storage.WithSession(context.Background(), "sess")
	result, err := def.Handler(ctx, map[string]any{"tasks": []any{
		map[string]any{"content": "step one", "status": "in_progress"},
		map[string]any{"content": "step two"},
	}})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	res, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map", result)
	}
	if res["complete"] != false {
		t.Errorf("complete = %v, want false", res["complete"])
	}

	// Persisted to the store.
	saved, _ := store.Load(ctx)
	if len(saved.Tasks) != 2 || saved.Tasks[0].Status != TaskInProgress {
		t.Errorf("saved plan = %+v, want 2 tasks with first in_progress", saved.Tasks)
	}

	// Stream event emitted.
	select {
	case evt := <-sub:
		if evt.Type != stream.EventPlanUpdate {
			t.Errorf("event type = %q, want %q", evt.Type, stream.EventPlanUpdate)
		}
	case <-time.After(time.Second):
		t.Error("expected a plan_update stream event")
	}
}

func TestPlanTool_HandlerRejectsBadArgs(t *testing.T) {
	def := NewPlanTool(NewInMemoryPlanStore(), nil)
	if _, err := def.Handler(storage.WithSession(context.Background(), "s"), map[string]any{"tasks": "bad"}); err == nil {
		t.Fatal("expected error for malformed tasks argument")
	}
}

func TestPlanToolkit_RegistersPlanTool(t *testing.T) {
	tk := NewPlanToolkit(NewInMemoryPlanStore(), nil)
	names := tk.ToolNames()
	if len(names) != 1 || names[0] != PlanToolName {
		t.Fatalf("toolkit tools = %v, want [%s]", names, PlanToolName)
	}
}

// Concurrent saves across many sessions must not race or corrupt state. Run with
// -race. Each goroutine owns its own session, mirroring the realistic
// many-sessions/one-writer pattern.
func TestInMemoryPlanStore_ConcurrentSaves_NoRace(t *testing.T) {
	store := NewInMemoryPlanStore()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			ctx := storage.WithSession(context.Background(), fmt.Sprintf("s%d", i))
			content := fmt.Sprintf("task-%d", i)
			if err := store.Save(ctx, &Plan{Tasks: []PlanTask{{Content: content, Status: TaskPending}}}); err != nil {
				t.Errorf("save: %v", err)
				return
			}
			got, err := store.Load(ctx)
			if err != nil {
				t.Errorf("load: %v", err)
				return
			}
			if len(got.Tasks) != 1 || got.Tasks[0].Content != content {
				t.Errorf("session %d plan = %+v, want [%s]", i, got.Tasks, content)
			}
		}(i)
	}
	wg.Wait()
}
