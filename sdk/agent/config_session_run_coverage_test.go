package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/hooks"
	"github.com/spawn08/chronos/engine/mcp"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/storage"
)

func TestChatWithSession_HookBeforeModelError(t *testing.T) {
	st := newTestStorage()
	a, _ := New("a", "a").
		WithModel(&testProvider{response: &model.ChatResponse{Content: "hi"}}).
		WithStorage(st).
		AddHook(hookBlocksModelCall{}).
		Build()

	_, err := a.ChatWithSession(context.Background(), "sess-hook", "hello")
	if err == nil {
		t.Fatal("expected hook error")
	}
	if !strings.Contains(err.Error(), "hook before model call") {
		t.Fatalf("unexpected err: %v", err)
	}
}

type hookBlocksModelCall struct{}

func (hookBlocksModelCall) Before(ctx context.Context, evt *hooks.Event) error {
	if evt.Type == hooks.EventModelCallBefore {
		return errors.New("blocked")
	}
	return nil
}

func (hookBlocksModelCall) After(context.Context, *hooks.Event) error { return nil }

func TestChatWithSession_PersistUserMessageError(t *testing.T) {
	base := newTestStorage()
	st := &failFirstAppendStore{testStorage: base}

	a, _ := New("a", "a").
		WithModel(&testProvider{response: &model.ChatResponse{Content: "hi"}}).
		WithStorage(st).
		Build()

	_, err := a.ChatWithSession(context.Background(), "sess-user-persist", "hello")
	if err == nil {
		t.Fatal("expected persist user message error")
	}
	if !errors.Is(err, errAppendBoom) {
		t.Fatalf("unexpected err: %v", err)
	}
}

var errAppendBoom = errors.New("append boom")

type failFirstAppendStore struct {
	*testStorage
	appends int
}

func (f *failFirstAppendStore) AppendEvent(ctx context.Context, e *storage.Event) error {
	f.appends++
	if f.appends == 1 {
		return errAppendBoom
	}
	return f.testStorage.AppendEvent(ctx, e)
}

func TestBuildAgent_EmptyModelProvider_Table(t *testing.T) {
	tests := []struct {
		name     string
		provider string
	}{
		{"empty", ""},
		{"whitespace", "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildAgent(context.Background(), &AgentConfig{
				ID:   "x",
				Name: "x",
				Model: ModelConfig{
					Provider: tt.provider,
					Model:    "gpt-4o",
				},
			})
			if err == nil {
				t.Fatal("expected model provider error")
			}
		})
	}
}

func TestRun_ModelExecuteError_Table(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
	}{
		{"with_message", map[string]any{"message": "hi"}},
		{"empty_message_uses_state_prompt", map[string]any{"topic": "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, _ := New("m", "m").
				WithModel(&testProvider{err: errors.New("chat failed")}).
				Build()

			_, err := a.Run(context.Background(), tt.input)
			if err == nil {
				t.Fatal("expected Execute/Run error")
			}
		})
	}
}

func TestRun_HookAfterRunError(t *testing.T) {
	g := graph.New("hookg")
	g.AddNode("n1", func(ctx context.Context, s graph.State) (graph.State, error) {
		out := graph.State{}
		for k, v := range s {
			out[k] = v
		}
		out["response"] = "ok"
		return out, nil
	})
	g.SetEntryPoint("n1")
	g.SetFinishPoint("n1")
	cg, err := g.Compile()
	if err != nil {
		t.Fatal(err)
	}

	h := &afterRunErrHook{}
	a, _ := New("g", "g").
		WithGraph(g).
		WithStorage(newTestStorage()).
		AddHook(h).
		Build()
	a.Graph = cg

	_, err = a.Run(context.Background(), map[string]any{"message": "hi"})
	if err == nil {
		t.Fatal("expected hook After error")
	}
	if !errors.Is(err, errHookAfter) {
		t.Fatalf("got %v", err)
	}
}

var errHookAfter = errors.New("hook after run")

type afterRunErrHook struct{}

func (afterRunErrHook) Before(context.Context, *hooks.Event) error { return nil }

func (afterRunErrHook) After(ctx context.Context, evt *hooks.Event) error {
	if evt.Type == hooks.EventNodeAfter {
		return errHookAfter
	}
	return nil
}

func TestCloseMCP_EmptyClientSlice(t *testing.T) {
	a, _ := New("c", "c").Build()
	a.MCPClients = []*mcp.Client{}
	a.CloseMCP()
}
