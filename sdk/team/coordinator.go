package team

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/protocol"
)

// coordinatorPlan is the structured output the coordinator LLM produces.
type coordinatorPlan struct {
	Tasks []coordinatorTask `json:"tasks"`
	Done  bool              `json:"done"`
}

type coordinatorTask struct {
	AgentID     string `json:"agent_id"`
	Description string `json:"description"`
	DependsOn   string `json:"depends_on,omitempty"`
}

// runCoordinator uses an LLM-driven supervisor to decompose the task, delegate
// sub-tasks to specialist agents, and optionally re-plan based on results.
//
// The coordinator can be:
//   - An explicit agent set via SetCoordinator()
//   - The first agent added to the team (backward-compatible)
//
// The coordinator receives a structured prompt containing agent descriptions and
// capabilities, and is asked to produce a JSON plan. It can iterate up to
// MaxIterations times, re-planning after seeing intermediate results.
//
// Sub-tasks without dependencies run in parallel; tasks with DependsOn wait
// for their dependency to complete. This gives the coordinator fine-grained
// control over execution order without requiring sequential-only delegation.
func (t *Team) runCoordinator(ctx context.Context, input graph.State) (graph.State, error) {
	if len(t.Order) < 1 {
		return nil, fmt.Errorf("team %q: coordinator strategy requires at least 1 agent", t.ID)
	}

	coordinator := t.Coordinator
	if coordinator == nil {
		if len(t.Order) < 2 {
			return nil, fmt.Errorf("team %q: coordinator strategy with no explicit coordinator requires at least 2 agents", t.ID)
		}
		coordinator = t.Agents[t.Order[0]]
	}

	if coordinator.Model == nil {
		return nil, fmt.Errorf("team %q: coordinator agent %q has no model", t.ID, coordinator.ID)
	}

	state := make(graph.State, len(input))
	for k, v := range input {
		state[k] = v
	}

	maxIter := t.MaxIterations
	if maxIter <= 0 {
		maxIter = 1
	}

	for iter := 0; iter < maxIter; iter++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		plan, err := t.coordinatorPlan(ctx, coordinator, state, iter)
		if err != nil {
			return nil, fmt.Errorf("team %q: coordinator plan (iteration %d): %w", t.ID, iter+1, err)
		}

		if len(plan.Tasks) == 0 || plan.Done {
			break
		}

		results, err := t.executePlan(ctx, coordinator.ID, plan, state)
		if err != nil {
			return nil, fmt.Errorf("team %q: execute plan (iteration %d): %w", t.ID, iter+1, err)
		}

		for k, v := range results {
			state[k] = v
		}

		if plan.Done {
			break
		}
	}

	return state, nil
}

// coordinatorPlan asks the coordinator LLM to produce a structured task decomposition.
func (t *Team) coordinatorPlan(ctx context.Context, coordinator *agent.Agent, state graph.State, iteration int) (*coordinatorPlan, error) {
	prompt := t.buildCoordinatorPrompt(state, iteration)

	req := &model.ChatRequest{
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: coordinatorSystemPrompt(t.agentInfoList())},
			{Role: model.RoleUser, Content: prompt},
		},
		ResponseFormat: "json_object",
	}

	resp, err := coordinator.Model.Chat(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("coordinator model call: %w", err)
	}

	var plan coordinatorPlan
	if err := json.Unmarshal([]byte(resp.Content), &plan); err != nil {
		// If JSON parse fails, try to extract from markdown code blocks
		cleaned := extractJSON(resp.Content)
		if err2 := json.Unmarshal([]byte(cleaned), &plan); err2 != nil {
			return nil, fmt.Errorf("parse coordinator plan: %w (raw: %s)", err, resp.Content)
		}
	}

	// Validate agent IDs in the plan
	for i, task := range plan.Tasks {
		if _, ok := t.Agents[task.AgentID]; !ok {
			return nil, fmt.Errorf("coordinator plan references unknown agent %q (task %d)", task.AgentID, i)
		}
	}

	return &plan, nil
}

// executePlan runs all tasks in the plan, respecting dependencies.
// Tasks without dependencies execute in parallel via the bus.
func (t *Team) executePlan(ctx context.Context, coordinatorID string, plan *coordinatorPlan, baseState graph.State) (graph.State, error) {
	taskResults := make(map[string]map[string]any)
	completed := make(map[string]bool)
	merged := make(graph.State)

	// Group tasks: independent vs dependent
	var independent []coordinatorTask
	var dependent []coordinatorTask
	for _, task := range plan.Tasks {
		if task.DependsOn == "" {
			independent = append(independent, task)
		} else {
			dependent = append(dependent, task)
		}
	}

	// Execute independent tasks via bus (parallel)
	for _, task := range independent {
		taskPayload := protocol.TaskPayload{
			Description: task.Description,
			Input:       baseState,
		}
		result, err := t.Bus.DelegateTask(ctx, coordinatorID, task.AgentID, "subtask", taskPayload)
		if err != nil {
			return nil, fmt.Errorf("delegate to %q: %w", task.AgentID, err)
		}
		if !result.Success {
			return nil, fmt.Errorf("agent %q failed: %s", task.AgentID, result.Error)
		}
		taskResults[task.AgentID] = result.Output
		completed[task.AgentID] = true
		for k, v := range result.Output {
			merged[k] = v
		}
	}

	// Execute dependent tasks in order
	for _, task := range dependent {
		if !completed[task.DependsOn] {
			return nil, fmt.Errorf("task for %q depends on %q which has not completed", task.AgentID, task.DependsOn)
		}

		// Merge dependency output into task input
		taskInput := make(map[string]any, len(baseState)+len(taskResults[task.DependsOn]))
		for k, v := range baseState {
			taskInput[k] = v
		}
		for k, v := range taskResults[task.DependsOn] {
			taskInput[k] = v
		}

		taskPayload := protocol.TaskPayload{
			Description: task.Description,
			Input:       taskInput,
		}
		result, err := t.Bus.DelegateTask(ctx, coordinatorID, task.AgentID, "subtask", taskPayload)
		if err != nil {
			return nil, fmt.Errorf("delegate to %q: %w", task.AgentID, err)
		}
		if !result.Success {
			return nil, fmt.Errorf("agent %q failed: %s", task.AgentID, result.Error)
		}
		taskResults[task.AgentID] = result.Output
		completed[task.AgentID] = true
		for k, v := range result.Output {
			merged[k] = v
		}
	}

	return merged, nil
}

func (t *Team) buildCoordinatorPrompt(state graph.State, iteration int) string {
	var b strings.Builder
	if iteration == 0 {
		b.WriteString("Analyze the following task and create an execution plan.\n\n")
	} else {
		b.WriteString(fmt.Sprintf("Iteration %d: Review the results so far and decide whether to continue or finalize.\n\n", iteration+1))
	}

	b.WriteString("Current state:\n")
	for k, v := range state {
		if strings.HasPrefix(k, "_") {
			continue
		}
		b.WriteString(fmt.Sprintf("  %s: %v\n", k, v))
	}

	b.WriteString("\nRespond with a JSON object containing:\n")
	b.WriteString(`{"tasks": [{"agent_id": "...", "description": "...", "depends_on": "..."}], "done": false}`)
	b.WriteString("\n\nSet \"done\": true if the task is complete and no more work is needed.")
	return b.String()
}

func coordinatorSystemPrompt(agents []AgentInfo) string {
	var b strings.Builder
	b.WriteString("You are a coordinator that decomposes tasks and delegates to specialist agents.\n\n")
	b.WriteString("Available agents:\n")
	for _, a := range agents {
		b.WriteString(fmt.Sprintf("- ID: %q, Name: %q, Description: %q", a.ID, a.Name, a.Description))
		if len(a.Capabilities) > 0 {
			b.WriteString(fmt.Sprintf(", Capabilities: %v", a.Capabilities))
		}
		b.WriteString("\n")
	}
	b.WriteString("\nCreate a plan with specific tasks for each agent. ")
	b.WriteString("Use depends_on to specify ordering constraints. ")
	b.WriteString("Tasks without depends_on will run in parallel.")
	return b.String()
}

// extractJSON attempts to extract a JSON object from text that may contain
// markdown code fences or other surrounding text.
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return s
	}
	depth := 0
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}
