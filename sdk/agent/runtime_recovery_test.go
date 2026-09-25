package agent_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/hooks"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/stream"
	"github.com/spawn08/chronos/engine/tool"
	chronostrace "github.com/spawn08/chronos/os/trace"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/harness"
	"github.com/spawn08/chronos/storage"
	memorystore "github.com/spawn08/chronos/storage/adapters/memory"
)

type runtimeProvider struct {
	id     string
	chat   func(context.Context, *model.ChatRequest) (*model.ChatResponse, error)
	stream func(context.Context, *model.ChatRequest) (<-chan *model.ChatResponse, error)
}

func (p *runtimeProvider) Name() string  { return "runtime-test" }
func (p *runtimeProvider) Model() string { return p.id }
func (p *runtimeProvider) Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	return p.chat(ctx, req)
}
func (p *runtimeProvider) StreamChat(ctx context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	if p.stream != nil {
		return p.stream(ctx, req)
	}
	resp, err := p.chat(ctx, req)
	if err != nil {
		return nil, err
	}
	ch := make(chan *model.ChatResponse, 1)
	ch <- resp
	close(ch)
	return ch, nil
}

type runtimeHook struct {
	before func(context.Context, *hooks.Event) error
	after  func(context.Context, *hooks.Event) error
}

func (h runtimeHook) Before(ctx context.Context, e *hooks.Event) error {
	if h.before != nil {
		return h.before(ctx, e)
	}
	return nil
}
func (h runtimeHook) After(ctx context.Context, e *hooks.Event) error {
	if h.after != nil {
		return h.after(ctx, e)
	}
	return nil
}

func runtimeChat(ctx context.Context, a *agent.Agent, streaming bool) (string, error) {
	if !streaming {
		return a.Execute(ctx, "task")
	}
	ch, err := a.ChatStream(ctx, "task")
	if err != nil {
		return "", err
	}
	var text strings.Builder
	for chunk := range ch {
		text.WriteString(chunk.Content)
		if chunk.Err != nil {
			err = chunk.Err
		}
	}
	return text.String(), err
}

// BL-004: retry the failed request, not the tools that produced its input.
func TestRuntimeRecovery_MutationAndTrimmedWorkingSet(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("streaming=%t", streaming), func(t *testing.T) {
			calls, mutations, before, after := 0, 0, 0, 0
			var failedMessages []model.Message
			p := &runtimeProvider{id: "initial-model"}
			p.chat = func(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
				calls++
				if req.Model != p.id {
					t.Errorf("request model = %q, live model = %q", req.Model, p.id)
				}
				for _, m := range req.Messages {
					if m.Content == "obsolete" || (calls > 1 && m.Content == "task") {
						t.Errorf("guard-trimmed message resurrected on call %d: %#v", calls, req.Messages)
					}
				}
				if calls == 2 {
					failedMessages = append([]model.Message(nil), req.Messages...)
					return nil, fmt.Errorf("provider: %w", &model.APIError{StatusCode: 503, Status: "unavailable"})
				}
				if calls == 3 && !reflect.DeepEqual(req.Messages, failedMessages) {
					t.Errorf("retry changed request: %#v", req.Messages)
				}
				if calls == 1 || calls == 3 {
					return &model.ChatResponse{StopReason: model.StopReasonToolCall, ToolCalls: []model.ToolCall{{ID: fmt.Sprint(calls), Name: "mutate", Arguments: `{}`}}}, nil
				}
				return &model.ChatResponse{Content: "done", StopReason: model.StopReasonEnd}, nil
			}
			h := runtimeHook{before: func(_ context.Context, e *hooks.Event) error {
				if e.Type == hooks.EventModelCallBefore {
					before++
					req := e.Input.(*model.ChatRequest)
					if req.Model != p.id {
						t.Errorf("hook received stale/empty model %q", req.Model)
					}
					if before <= 2 {
						req.Messages = append([]model.Message(nil), req.Messages[1:]...)
					}
				}
				return nil
			}, after: func(_ context.Context, e *hooks.Event) error {
				if e.Type == hooks.EventModelCallAfter {
					after++
				}
				return nil
			}}
			a, err := agent.New("a", "A").WithModel(p).WithSystemPrompt("obsolete").AddHook(h).
				AddTool(&tool.Definition{Name: "mutate", Permission: tool.PermAllow, Handler: func(context.Context, map[string]any) (any, error) {
					mutations++
					p.id = "switched-model"
					return "mutation committed", nil
				}}).Build()
			if err != nil {
				t.Fatal(err)
			}
			text, err := runtimeChat(context.Background(), a, streaming)
			if err != nil || text != "done" {
				t.Fatalf("response = %q, err = %v", text, err)
			}
			if calls != 4 || mutations != 2 || before != 3 || after != 3 {
				t.Fatalf("calls=%d mutations=%d hooks=%d/%d; want 4,2,3/3", calls, mutations, before, after)
			}
		})
	}
}

func TestRuntimeRecovery_ClassificationAndBound(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		calls int
	}{
		{"408", &model.APIError{StatusCode: 408}, 2},
		{"429", &model.APIError{StatusCode: 429}, 2},
		{"503", &model.APIError{StatusCode: 503}, 2},
		{"timeout", &net.DNSError{IsTimeout: true}, 2},
		{"unexpected EOF", io.ErrUnexpectedEOF, 2},
		{"400", &model.APIError{StatusCode: 400}, 1},
		{"401", &model.APIError{StatusCode: 401}, 1},
		{"canceled", context.Canceled, 1},
		{"circuit open", model.ErrCircuitOpen, 1},
		{"untyped", errors.New("rate limit 429"), 1},
		{"long retry-after", &model.APIError{StatusCode: 429, RetryAfter: time.Hour}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			p := &runtimeProvider{id: "model", chat: func(context.Context, *model.ChatRequest) (*model.ChatResponse, error) {
				calls++
				return nil, tc.err
			}}
			a, _ := agent.New("a", "A").WithModel(p).Build()
			_, err := a.Execute(context.Background(), "task")
			if !errors.Is(err, tc.err) || calls != tc.calls {
				t.Fatalf("calls=%d err=%v, want %d and original error", calls, err, tc.calls)
			}
		})
	}
}

func TestRuntimeRecovery_CancellationAndGuardError(t *testing.T) {
	for _, mode := range []string{"before call", "during backoff", "guard rejection"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			called := make(chan struct{}, 1)
			p := &runtimeProvider{id: "model", chat: func(context.Context, *model.ChatRequest) (*model.ChatResponse, error) {
				calls++
				called <- struct{}{}
				return nil, &model.APIError{StatusCode: 503, RetryAfter: time.Second}
			}}
			a, _ := agent.New("a", "A").WithModel(p).Build()
			wantCalls := 0
			wantErr := error(context.Canceled)
			switch mode {
			case "before call":
				cancel()
			case "during backoff":
				wantCalls = 1
				go func() { <-called; cancel() }()
			case "guard rejection":
				wantErr = errors.New("context guard rejected request")
				a.Hooks = hooks.Chain{runtimeHook{before: func(context.Context, *hooks.Event) error { return wantErr }}}
			}
			start := time.Now()
			_, err := a.Execute(ctx, "task")
			if !errors.Is(err, wantErr) || calls != wantCalls || time.Since(start) > 500*time.Millisecond {
				t.Fatalf("calls=%d err=%v elapsed=%v", calls, err, time.Since(start))
			}
		})
	}
}

func TestRuntimeRecovery_NoRetryAfterStreamEmission(t *testing.T) {
	for _, reasoning := range []bool{false, true} {
		t.Run(fmt.Sprintf("reasoning=%t", reasoning), func(t *testing.T) {
			streamCalls, chatCalls := 0, 0
			sentinel := &model.APIError{StatusCode: 503, Status: "failed after emission"}
			p := &runtimeProvider{id: "model", chat: func(context.Context, *model.ChatRequest) (*model.ChatResponse, error) {
				chatCalls++
				return &model.ChatResponse{Content: "duplicated", StopReason: model.StopReasonEnd}, nil
			}, stream: func(context.Context, *model.ChatRequest) (<-chan *model.ChatResponse, error) {
				streamCalls++
				ch := make(chan *model.ChatResponse, 2)
				chunk := &model.ChatResponse{Content: "partial", Delta: true}
				if reasoning {
					chunk.Content, chunk.Reasoning = "", "partial reasoning"
				}
				ch <- chunk
				ch <- &model.ChatResponse{Err: sentinel}
				close(ch)
				return ch, nil
			}}
			retry := hooks.NewRetryHook(3)
			retry.SleepFn = func(time.Duration) {}
			a, _ := agent.New("a", "A").WithModel(p).AddHook(retry).
				WithReasoningConfig(model.ReasoningConfig{Summary: true}).Build()
			_, err := runtimeChat(context.Background(), a, true)
			if !errors.Is(err, sentinel) || streamCalls != 1 || chatCalls != 0 || retry.RetriesCount() != 0 {
				t.Fatalf("err=%v stream calls=%d chat calls=%d hook retries=%d", err, streamCalls, chatCalls, retry.RetriesCount())
			}
		})
	}
}

func TestRuntimeRecovery_DynamicDelegateLiveResources(t *testing.T) {
	ctx := storage.WithSession(storage.WithTenant(context.Background(), "tenant"), "session")
	store := memorystore.New()
	broker := stream.NewBroker()
	events := broker.Subscribe("test")
	oldCalls, newCalls, hookCalls, approvals, mutations := 0, 0, 0, 0, 0
	old := &runtimeProvider{id: "old", chat: func(context.Context, *model.ChatRequest) (*model.ChatResponse, error) {
		oldCalls++
		return &model.ChatResponse{Content: "old", StopReason: model.StopReasonEnd}, nil
	}}
	parent, _ := agent.New("parent", "Parent").WithModel(old).WithStorage(store).
		WithBroker(broker).WithTracer(chronostrace.NewCollector(store)).WithHistoryRuns(3).
		WithSystemPrompt("private parent conversation").WithContextConfig(agent.ContextConfig{
		MaxContextTokens: 1234, PinnedMessages: []model.Message{{Role: model.RoleSystem, Content: "operational pin"}},
	}).AddTool(&tool.Definition{Name: "mutate", Permission: tool.PermRequireApproval, RequiresConfirmation: true,
		Handler: func(ctx context.Context, _ map[string]any) (any, error) {
			if storage.TenantFromContext(ctx) != "tenant" || storage.SessionFromContext(ctx) != "session" {
				t.Error("delegate lost caller context")
			}
			mutations++
			return "committed", nil
		}}).Build()
	parent.Tools.SetApprovalHandler(func(context.Context, string, map[string]any) (bool, error) { approvals++; return true, nil })
	svc, err := harness.NewSubAgentService(parent)
	if err != nil {
		t.Fatal(err)
	}
	// Configure the model/hooks after service construction, as runtime switching does.
	parent.Model = &runtimeProvider{id: "new", chat: func(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
		newCalls++
		if req.Model != "new" || req.Messages[0].Content != "delegate role" || req.Messages[1].Content != "operational pin" {
			t.Errorf("delegate request = %#v", req)
		}
		for _, m := range req.Messages {
			if strings.Contains(m.Content, "private parent") {
				t.Error("delegate inherited conversation")
			}
		}
		if newCalls == 1 {
			return &model.ChatResponse{StopReason: model.StopReasonToolCall, ToolCalls: []model.ToolCall{{ID: "1", Name: "mutate", Arguments: `{}`}}}, nil
		}
		return &model.ChatResponse{Content: "new result", StopReason: model.StopReasonEnd}, nil
	}}
	parent.Hooks = hooks.Chain{runtimeHook{before: func(ctx context.Context, e *hooks.Event) error {
		if e.Type == hooks.EventModelCallBefore {
			hookCalls++
			if storage.TenantFromContext(ctx) != "tenant" {
				t.Error("hook lost tenant")
			}
		}
		return nil
	}}}
	result, err := harness.NewInProcessRunner(svc).Run(ctx, harness.SubAgentSpec{Name: "worker", SystemPrompt: "delegate role", ToolNames: []string{"mutate"}}, "delegated task")
	if err != nil || result != "new result" || oldCalls != 0 || newCalls != 2 || hookCalls != 2 || approvals != 1 || mutations != 1 {
		t.Fatalf("result=%q err=%v old/new=%d/%d hooks=%d approvals=%d mutations=%d", result, err, oldCalls, newCalls, hookCalls, approvals, mutations)
	}
	select {
	case <-events:
	default:
		t.Error("delegate did not use parent broker")
	}
	traces, err := store.ListTraces(ctx, "session")
	if err != nil || len(traces) != 3 {
		t.Fatalf("shared trace storage: traces=%d err=%v", len(traces), err)
	}
}

type runtimeSharedStorage struct {
	storage.Storage
	migrations int
	closes     int
}

func (s *runtimeSharedStorage) Migrate(context.Context) error { s.migrations++; return nil }
func (s *runtimeSharedStorage) Close() error                  { s.closes++; return nil }

func TestRuntimeRecovery_BuildAllSharedStorage(t *testing.T) {
	ctx := context.Background()
	shared := &runtimeSharedStorage{Storage: memorystore.New()}
	// This default DSN cannot be opened. Injection must bypass construction,
	// rather than replacing already-constructed per-agent stores afterward.
	defaultConfig := agent.StorageConfig{Backend: "sqlite", DSN: filepath.Join(t.TempDir(), "missing", "default.db")}
	fc := &agent.FileConfig{Defaults: &agent.AgentConfig{Storage: defaultConfig}, Agents: []agent.AgentConfig{
		{ID: "default-a", Model: agent.ModelConfig{Provider: "openai"}, Storage: defaultConfig, Tracing: true},
		{ID: "default-b", Model: agent.ModelConfig{Provider: "openai"}, Storage: defaultConfig},
		{ID: "empty", Model: agent.ModelConfig{Provider: "openai"}},
		{ID: "custom", Model: agent.ModelConfig{Provider: "openai"}, Storage: agent.StorageConfig{Backend: "sqlite", DSN: ":memory:"}},
		{ID: "disabled", Model: agent.ModelConfig{Provider: "openai"}, Storage: agent.StorageConfig{Backend: "none"}},
	}}
	fc.Agents[0].Tools = []agent.ToolConfig{{Name: "custom_handler"}}
	agents, err := agent.BuildAllWithOptions(ctx, fc, agent.BuildAllOptions{DefaultStorage: shared},
		agent.WithToolHandler("custom_handler", func(agent.ToolConfig) (tool.Handler, error) {
			return func(context.Context, map[string]any) (any, error) { return "bound", nil }, nil
		}))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"default-a", "default-b", "empty"} {
		if agents[id].Storage != shared {
			t.Errorf("%s did not receive shared storage", id)
		}
	}
	custom := agents["custom"].Storage
	if custom == nil || custom == shared || agents["disabled"].Storage != nil {
		t.Fatal("custom/disabled storage semantics changed")
	}
	t.Cleanup(func() { _ = custom.Close() })
	if _, err := custom.ListSessions(ctx, "a", 1, 0); err != nil {
		t.Fatalf("custom store was not migrated: %v", err)
	}
	if shared.migrations != 0 || shared.closes != 0 || agents["default-a"].Tracer == nil {
		t.Fatalf("shared lifecycle: migrations=%d closes=%d tracer=%v", shared.migrations, shared.closes, agents["default-a"].Tracer)
	}
	if got, err := agents["default-a"].Tools.Execute(ctx, "custom_handler", nil); err != nil || got != "bound" {
		t.Fatalf("existing BuildOption lost: %v, %v", got, err)
	}
	fc.Agents = []agent.AgentConfig{fc.Agents[0], {ID: "broken", Model: agent.ModelConfig{Provider: "unknown"}}}
	if _, err := agent.BuildAllWithOptions(ctx, fc, agent.BuildAllOptions{DefaultStorage: shared}); err == nil {
		t.Fatal("expected failed build")
	}
	if shared.closes != 0 {
		t.Fatal("failed build closed caller-owned storage")
	}
}

func TestRuntimeRecovery_ExplicitRetryHookDoesNotMultiplyRetries(t *testing.T) {
	calls := 0
	terminal := &model.APIError{StatusCode: 401, Status: "unauthorized"}
	p := &runtimeProvider{id: "model", chat: func(context.Context, *model.ChatRequest) (*model.ChatResponse, error) {
		calls++
		if calls == 1 {
			return nil, &model.APIError{StatusCode: 503, Status: "unavailable"}
		}
		return nil, terminal
	}}
	retry := hooks.NewRetryHook(1)
	retry.SleepFn = func(time.Duration) {}
	a, _ := agent.New("a", "A").WithModel(p).AddHook(retry).Build()
	_, err := a.Execute(context.Background(), "task")
	if !errors.Is(err, terminal) || calls != 2 || retry.RetriesCount() != 1 {
		t.Fatalf("err=%v calls=%d hook retries=%d", err, calls, retry.RetriesCount())
	}
}

func TestRuntimeRecovery_ProviderRetriesRemainBounded(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"unavailable"}}`))
	}))
	defer server.Close()
	provider := model.NewOpenAIWithConfig(model.ProviderConfig{Model: "test-model", BaseURL: server.URL, MaxRetries: 1})
	a, _ := agent.New("a", "A").WithModel(provider).Build()
	_, err := a.Execute(context.Background(), "task")
	var apiErr *model.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 503 || requests.Load() != 4 {
		t.Fatalf("err=%v requests=%d, want at most 2*(1+1) HTTP attempts", err, requests.Load())
	}
}

func TestRuntimeRecovery_DurableRequestDoesNotRetryAcrossSDKHookOrHTTP(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"unavailable"}}`))
	}))
	defer server.Close()
	provider := model.NewOpenAIWithConfig(model.ProviderConfig{Model: "test-model", BaseURL: server.URL, MaxRetries: 3})
	retry := hooks.NewRetryHook(3)
	retry.SleepFn = func(time.Duration) {}
	a, _ := agent.New("a", "A").WithModel(provider).AddHook(retry).Build()
	_, err := a.Execute(agent.WithModelRetriesDisabled(context.Background()), "task")
	var apiErr *model.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 503 || requests.Load() != 1 || retry.RetriesCount() != 0 {
		t.Fatalf("durable call err=%v HTTP attempts=%d retry-hook attempts=%d", err, requests.Load(), retry.RetriesCount())
	}
}

func TestRuntimeRecovery_StreamBodyFailureBeforeEmission(t *testing.T) {
	calls := 0
	p := &runtimeProvider{id: "model", stream: func(context.Context, *model.ChatRequest) (<-chan *model.ChatResponse, error) {
		calls++
		ch := make(chan *model.ChatResponse, 1)
		if calls == 1 {
			ch <- &model.ChatResponse{Err: io.ErrUnexpectedEOF}
		} else {
			ch <- &model.ChatResponse{Content: "recovered", StopReason: model.StopReasonEnd}
		}
		close(ch)
		return ch, nil
	}}
	a, _ := agent.New("a", "A").WithModel(p).Build()
	text, err := runtimeChat(context.Background(), a, true)
	if err != nil || text != "recovered" || calls != 2 {
		t.Fatalf("text=%q err=%v calls=%d", text, err, calls)
	}
}

func TestRuntimeRecovery_DelegatePermissionModes(t *testing.T) {
	for _, tc := range []struct {
		mode       tool.PermissionMode
		permission tool.Permission
		mutations  int
	}{
		{tool.PermissionModeAutoApprove, tool.PermRequireApproval, 1},
		{tool.PermissionModeDeny, tool.PermRequireApproval, 0},
		{tool.PermissionModeAutoApprove, tool.PermDeny, 0},
	} {
		t.Run(string(tc.mode)+"/"+string(tc.permission), func(t *testing.T) {
			calls, mutations, approvals := 0, 0, 0
			p := &runtimeProvider{id: "model", chat: func(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
				calls++
				if calls == 1 {
					if len(req.Tools) != 1 || req.Tools[0].Function.Name != "granted" {
						t.Errorf("delegate tool subset = %#v", req.Tools)
					}
					return &model.ChatResponse{StopReason: model.StopReasonToolCall, ToolCalls: []model.ToolCall{{ID: "1", Name: "granted", Arguments: `{}`}}}, nil
				}
				return &model.ChatResponse{Content: req.Messages[len(req.Messages)-1].Content, StopReason: model.StopReasonEnd}, nil
			}}
			parent, _ := agent.New("parent", "Parent").WithModel(p).
				AddTool(&tool.Definition{Name: "granted", Permission: tc.permission, Handler: func(context.Context, map[string]any) (any, error) {
					mutations++
					return "committed", nil
				}}).
				AddTool(&tool.Definition{Name: "ungranted", Permission: tool.PermAllow}).Build()
			svc, err := harness.NewSubAgentService(parent)
			if err != nil {
				t.Fatal(err)
			}
			if err := parent.Tools.SetPermissionMode(tc.mode); err != nil {
				t.Fatal(err)
			}
			parent.Tools.SetApprovalHandler(func(context.Context, string, map[string]any) (bool, error) { approvals++; return true, nil })
			result, err := harness.NewInProcessRunner(svc).Run(context.Background(), harness.SubAgentSpec{Name: "worker", SystemPrompt: "role", ToolNames: []string{"granted"}}, "task")
			if err != nil || mutations != tc.mutations || approvals != 0 {
				t.Fatalf("result=%q err=%v mutations=%d approvals=%d", result, err, mutations, approvals)
			}
			if tc.mutations == 0 && !strings.Contains(result, "Error:") {
				t.Fatalf("denied tool did not return an error: %q", result)
			}
		})
	}
}
