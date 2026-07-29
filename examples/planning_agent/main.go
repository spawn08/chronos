// Example: planning_agent demonstrates the built-in planning ("todo") tool
// (WC-A-001). An agent works a multi-step task, writing a structured plan and
// marking steps done as it progresses. The plan is persisted durably per session
// via builtins.NewStoragePlanStore, so it survives a checkpoint/resume — the
// example proves this by reloading the plan from a fresh store after the run.
//
// A small deterministic mock model.Provider drives the loop, so the example runs
// with NO API keys and NO network access. On each turn the mock advances the plan
// based on how many tasks are already completed:
//
//	turn 0: create the 3-step plan (step 1 in_progress)
//	turn 1: step 1 done, step 2 in_progress
//	turn 2: step 2 done, step 3 in_progress
//	turn 3: all steps done, then emit the final answer
//
//	go run ./examples/planning_agent/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool/builtins"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

const sessionID = "research-session-1"

func main() {
	ctx := context.Background()

	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║      Chronos Planning (Todo) Tool Example              ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	// A file-backed DB so we can genuinely simulate a process restart: the first
	// store runs the task, then we close it and reopen a fresh store over the same
	// file — exactly what a worker resuming the session on another process does.
	dir, err := os.MkdirTemp("", "chronos-planning-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	dbPath := filepath.Join(dir, "chronos.db")

	runTask(ctx, dbPath)
	reloadAfterRestart(ctx, dbPath)
}

// runTask opens a store, runs the multi-step task, and closes the store —
// standing in for the process that first executes the session.
func runTask(ctx context.Context, dbPath string) {
	store, err := sqlite.New(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	a, err := agent.New("research-agent", "Research Assistant").
		WithModel(&planningMock{}).
		WithStorage(store).
		WithSystemPrompt("You research topics and produce a report. Keep a plan.").
		AddToolkit(builtins.NewPlanToolkit(builtins.NewStoragePlanStore(store), nil)).
		Build()
	if err != nil {
		log.Fatal(err)
	}

	task := "Research the history of the Go programming language and write a short report."
	fmt.Printf("\nUser: %s\n\n", task)

	resp, err := a.ChatWithSession(ctx, sessionID, task)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nAssistant: %s\n", resp.Content)
}

// reloadAfterRestart opens a brand-new store over the same file and reads the
// plan back, proving it persisted durably across the "restart".
func reloadAfterRestart(ctx context.Context, dbPath string) {
	fmt.Println("\n── Reopening the store in a fresh process (simulating resume) ──")
	store, err := sqlite.New(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	resumeCtx := storage.WithSession(ctx, sessionID)
	reloaded, err := builtins.NewStoragePlanStore(store).Load(resumeCtx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(reloaded.Summary())
	if reloaded.Complete() {
		fmt.Println("\n✓ Plan persisted across restart and all steps are complete.")
	} else {
		fmt.Println("\n(plan reloaded, still in progress)")
	}
}

// planningMock is a deterministic model.Provider that maintains a 3-step plan via
// the update_plan tool, advancing one step per turn until the task is done.
type planningMock struct{}

var planSteps = []string{
	"Gather sources on Go's history",
	"Draft the report",
	"Review and finalize",
}

func (m *planningMock) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	// turn is how many times the plan has already been written. It advances the
	// plan one step per turn: steps before it are done, the step at it is active.
	turn := countPlanUpdates(req.Messages)

	// After the final all-completed plan has been written, produce the answer.
	if turn > len(planSteps) {
		return &model.ChatResponse{
			Role:       model.RoleAssistant,
			Content:    "Report complete: Go was created at Google in 2007 by Griesemer, Pike, and Thompson, and released in 2009.",
			StopReason: model.StopReasonEnd,
		}, nil
	}

	tasks := make([]any, len(planSteps))
	for i, content := range planSteps {
		status := string(builtins.TaskPending)
		switch {
		case i < turn:
			status = string(builtins.TaskCompleted)
		case i == turn:
			status = string(builtins.TaskInProgress)
		}
		tasks[i] = map[string]any{"content": content, "status": status}
	}
	return toolCallResponse(fmt.Sprintf("call_%d", turn), builtins.PlanToolName,
		map[string]any{"tasks": tasks}), nil
}

func (m *planningMock) StreamChat(ctx context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	ch := make(chan *model.ChatResponse, 1)
	resp, err := m.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	ch <- resp
	close(ch)
	return ch, nil
}

func (m *planningMock) Name() string  { return "planning-mock" }
func (m *planningMock) Model() string { return "mock-v1" }

// countPlanUpdates returns how many update_plan tool results are in the history,
// i.e. how many times the plan has been written so far this session.
func countPlanUpdates(messages []model.Message) int {
	count := 0
	for i := range messages {
		if messages[i].Role == model.RoleTool && messages[i].Name == builtins.PlanToolName {
			count++
		}
	}
	return count
}

func toolCallResponse(id, name string, args map[string]any) *model.ChatResponse {
	raw, _ := json.Marshal(args)
	return &model.ChatResponse{
		Role:       model.RoleAssistant,
		StopReason: model.StopReasonToolCall,
		ToolCalls:  []model.ToolCall{{ID: id, Name: name, Arguments: string(raw)}},
	}
}
