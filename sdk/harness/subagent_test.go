package harness

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/sdk/agent"
)

// recordingMock is a deterministic model.Provider for tests. reply is returned
// as the assistant message; every Chat request's messages are recorded so tests
// can assert what context the model actually saw.
type recordingMock struct {
	name  string
	reply string

	mu       sync.Mutex
	requests [][]model.Message
}

func (m *recordingMock) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	m.mu.Lock()
	cp := make([]model.Message, len(req.Messages))
	copy(cp, req.Messages)
	m.requests = append(m.requests, cp)
	m.mu.Unlock()
	return &model.ChatResponse{Role: model.RoleAssistant, Content: m.reply, StopReason: model.StopReasonEnd}, nil
}

func (m *recordingMock) StreamChat(ctx context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	ch := make(chan *model.ChatResponse, 1)
	resp, _ := m.Chat(ctx, req)
	ch <- resp
	close(ch)
	return ch, nil
}

func (m *recordingMock) Name() string  { return m.name }
func (m *recordingMock) Model() string { return "mock-v1" }

func (m *recordingMock) lastRequest() []model.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.requests) == 0 {
		return nil
	}
	return m.requests[len(m.requests)-1]
}

// newParent builds a parent agent with the given model and a dummy tool named
// "echo" the subagent may be granted.
func newParent(t *testing.T, m model.Provider) *agent.Agent {
	t.Helper()
	a, err := agent.New("parent", "Parent").
		WithModel(m).
		AddTool(&tool.Definition{
			Name:        "echo",
			Description: "echo",
			Permission:  tool.PermAllow,
			Parameters:  map[string]any{"type": "object"},
			Handler:     func(_ context.Context, _ map[string]any) (any, error) { return "echoed", nil },
		}).
		Build()
	if err != nil {
		t.Fatalf("build parent: %v", err)
	}
	return a
}

func TestSubAgent_ContextIsolation(t *testing.T) {
	subModel := &recordingMock{name: "sub", reply: "the answer is 42"}
	parent := newParent(t, &recordingMock{name: "parent"})
	svc, err := NewSubAgentService(parent)
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	if err = svc.Register(SubAgentSpec{Name: "researcher", SystemPrompt: "You are a researcher."}); err != nil {
		t.Fatalf("register: %v", err)
	}
	// Point the registered subagent at its own recording model.
	svc.model = subModel

	tk := NewSpawnSubAgentTool(svc, NewInProcessRunner(svc))
	res, err := tk.Handler(context.Background(), map[string]any{
		"agent": "researcher",
		"task":  "What is the meaning of life?",
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	// The parent receives only the final result.
	out := res.(map[string]any)
	if out["result"] != "the answer is 42" {
		t.Errorf("result = %v, want the subagent's final answer", out["result"])
	}

	// The subagent saw a FRESH context: only its own system prompt + the task,
	// never any of the parent's conversation.
	saw := subModel.lastRequest()
	if len(saw) != 2 {
		t.Fatalf("subagent saw %d messages, want 2 (system + task): %+v", len(saw), saw)
	}
	if saw[0].Role != model.RoleSystem || saw[0].Content != "You are a researcher." {
		t.Errorf("subagent msg[0] = %+v, want its own system prompt", saw[0])
	}
	if saw[1].Role != model.RoleUser || !strings.Contains(saw[1].Content, "meaning of life") {
		t.Errorf("subagent msg[1] = %+v, want the delegated task", saw[1])
	}
}

func TestSubAgent_DynamicSpawn(t *testing.T) {
	subModel := &recordingMock{name: "sub", reply: "done"}
	parent := newParent(t, &recordingMock{name: "parent"})
	svc, _ := NewSubAgentService(parent)
	svc.model = subModel

	tk := NewSpawnSubAgentTool(svc, NewInProcessRunner(svc))

	// A subagent invented at call time, granted the parent's "echo" tool.
	res, err := tk.Handler(context.Background(), map[string]any{
		"task":          "summarize",
		"system_prompt": "You are a summarizer.",
		"tools":         []any{"echo"},
	})
	if err != nil {
		t.Fatalf("dynamic spawn: %v", err)
	}
	if res.(map[string]any)["result"] != "done" {
		t.Errorf("result = %v, want done", res.(map[string]any)["result"])
	}
	if saw := subModel.lastRequest(); saw[0].Content != "You are a summarizer." {
		t.Errorf("dynamic subagent system prompt = %q", saw[0].Content)
	}
}

func TestSubAgent_DynamicRejectsUnknownTool(t *testing.T) {
	parent := newParent(t, &recordingMock{name: "parent"})
	svc, _ := NewSubAgentService(parent)
	tk := NewSpawnSubAgentTool(svc, NewInProcessRunner(svc))
	_, err := tk.Handler(context.Background(), map[string]any{
		"task":          "x",
		"system_prompt": "y",
		"tools":         []any{"nonexistent"},
	})
	if err == nil {
		t.Fatal("expected error for unknown tool grant")
	}
}

func TestSubAgent_RequiresTaskAndSpec(t *testing.T) {
	parent := newParent(t, &recordingMock{name: "parent"})
	svc, _ := NewSubAgentService(parent)
	tk := NewSpawnSubAgentTool(svc, NewInProcessRunner(svc))

	if _, err := tk.Handler(context.Background(), map[string]any{"task": ""}); err == nil {
		t.Error("expected error for empty task")
	}
	// No registered agent and no system_prompt → cannot build a subagent.
	if _, err := tk.Handler(context.Background(), map[string]any{"task": "do it"}); err == nil {
		t.Error("expected error when neither a registered agent nor a dynamic spec is given")
	}
}

func TestSubAgent_DepthGuard(t *testing.T) {
	parent := newParent(t, &recordingMock{name: "parent"})
	svc, _ := NewSubAgentService(parent, WithMaxDepth(2))
	svc.model = &recordingMock{name: "sub", reply: "ok"}
	tk := NewSpawnSubAgentTool(svc, NewInProcessRunner(svc))

	// At the depth limit, spawning is refused.
	ctx := withDepth(context.Background(), 2)
	if _, err := tk.Handler(ctx, map[string]any{"agent": "x", "task": "t", "system_prompt": "p"}); err == nil {
		t.Fatal("expected depth-limit error")
	}
	// Below the limit it proceeds.
	ctx = withDepth(context.Background(), 1)
	if _, err := tk.Handler(ctx, map[string]any{"task": "t", "system_prompt": "p"}); err != nil {
		t.Fatalf("spawn below depth limit: %v", err)
	}
}

// depthRecorder is a Runner that captures the recursion depth carried on the
// context when the spawn handler invokes it.
type depthRecorder struct{ got int }

func (d *depthRecorder) Run(ctx context.Context, _ SubAgentSpec, _ string) (string, error) {
	d.got = depthFromContext(ctx)
	return "ok", nil
}

// The spawn handler increments the depth before invoking the runner, so a nested
// spawn runs one level deeper — the propagation CRITICAL-Q01/NOTE-D02 guards.
func TestSubAgent_DepthIncrementsForRunner(t *testing.T) {
	parent := newParent(t, &recordingMock{name: "parent"})
	svc, _ := NewSubAgentService(parent)
	rec := &depthRecorder{}
	tk := NewSpawnSubAgentTool(svc, rec)

	if _, err := tk.Handler(withDepth(context.Background(), 2), map[string]any{
		"task": "t", "system_prompt": "p",
	}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if rec.got != 3 {
		t.Errorf("runner saw depth %d, want 3 (handler must pass depth+1)", rec.got)
	}
}

// stateDepth rehydrates the depth carried across the queue boundary, tolerating
// the float64 a JSON round-trip produces.
func TestStateDepth(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want int
	}{
		{"int", 4, 4},
		{"float64 from json", float64(2), 2},
		{"absent", nil, 0},
		{"wrong type", "nope", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := map[string]any{}
			if tt.val != nil {
				s[stateSubAgentDepth] = tt.val
			}
			if got := stateDepth(s); got != tt.want {
				t.Errorf("stateDepth(%v) = %d, want %d", tt.val, got, tt.want)
			}
		})
	}
}

// A typo'd registered name fails closed instead of silently spawning an ad-hoc
// agent (NOTE-D03/Q05).
func TestSubAgent_UnknownRegisteredNameFailsClosed(t *testing.T) {
	parent := newParent(t, &recordingMock{name: "parent"})
	svc, _ := NewSubAgentService(parent)
	_ = svc.Register(SubAgentSpec{Name: "researcher", SystemPrompt: "p"})
	tk := NewSpawnSubAgentTool(svc, NewInProcessRunner(svc))

	// "resercher" is a typo; even with a system_prompt present it must error, not
	// degrade to a dynamic agent.
	_, err := tk.Handler(context.Background(), map[string]any{
		"agent": "resercher", "task": "t", "system_prompt": "p",
	})
	if err == nil {
		t.Fatal("expected an error for an unknown registered subagent name")
	}
}

// Concurrent spawns must not race and must not cross results. Run with -race.
func TestSubAgent_ConcurrentSpawns_NoRace(t *testing.T) {
	parent := newParent(t, &recordingMock{name: "parent"})

	const n = 40
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			// Each spawn is a distinct dynamic subagent with its own model, so a
			// crossed result would be observable.
			reply := fmt.Sprintf("result-%d", i)
			svc := &SubAgentService{
				model:      &recordingMock{name: "sub", reply: reply},
				tools:      parent.Tools,
				registered: map[string]SubAgentSpec{},
				maxDepth:   DefaultMaxSubAgentDepth,
			}
			spawn := NewSpawnSubAgentTool(svc, NewInProcessRunner(svc))
			res, err := spawn.Handler(context.Background(), map[string]any{"task": "t", "system_prompt": "p"})
			if err != nil {
				errs <- err
				return
			}
			if got := res.(map[string]any)["result"]; got != reply {
				errs <- fmt.Errorf("got %v want %v", got, reply)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// Register, catalog reads, and spawns run concurrently on ONE shared service to
// exercise the registry mutex. Run with -race.
func TestSubAgentService_ConcurrentRegisterAndSpawn_NoRace(t *testing.T) {
	parent := newParent(t, &recordingMock{name: "parent"})
	svc, _ := NewSubAgentService(parent)
	svc.model = &recordingMock{name: "sub", reply: "ok"}
	spawn := NewSpawnSubAgentTool(svc, NewInProcessRunner(svc))

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(3)
		go func(i int) {
			defer wg.Done()
			_ = svc.Register(SubAgentSpec{Name: fmt.Sprintf("a%d", i), SystemPrompt: "p"})
		}(i)
		go func() { defer wg.Done(); _ = svc.Registered() }()
		go func() {
			defer wg.Done()
			_, _ = spawn.Handler(context.Background(), map[string]any{"task": "t", "system_prompt": "p"})
		}()
	}
	wg.Wait()
}
