package repl

import (
	"strings"
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
)

func TestSetAgent_NilAgent_ModelAndAgentHandlers_ITER6(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	r.SetAgent(nil)

	out := captureStdout(t, func() {
		_ = r.commands["/model"].Handler("")
	})
	if !strings.Contains(out, "No model configured") {
		t.Errorf("/model: want 'No model configured', got %q", out)
	}

	out2 := captureStdout(t, func() {
		_ = r.commands["/agent"].Handler("")
	})
	if !strings.Contains(out2, "No agent loaded") {
		t.Errorf("/agent: want 'No agent loaded', got %q", out2)
	}
}

func TestSetAgent_WithModel_ModelHandler_ITER6(t *testing.T) {
	store := newTestStore(t)
	r := New(store)

	prov := &mockProvider{resp: &model.ChatResponse{Content: "x"}}
	a, err := agent.New("a1", "NamedAgent").WithModel(prov).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	r.SetAgent(a)

	out := captureStdout(t, func() {
		_ = r.commands["/model"].Handler("")
	})
	if !strings.Contains(out, "mock") || !strings.Contains(out, "mock-model") {
		t.Errorf("/model output: %q", out)
	}
}

func TestSlashAgent_LongSystemPrompt_Truncated_ITER6(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	long := strings.Repeat("a", 120)
	r.SetAgent(&agent.Agent{
		ID: "x", Name: "Y", SystemPrompt: long,
	})

	out := captureStdout(t, func() {
		_ = r.commands["/agent"].Handler("")
	})
	if !strings.Contains(out, "...") {
		t.Errorf("expected truncated system prompt (ellipsis) in output: %q", out)
	}
	if !strings.Contains(out, "System:") {
		t.Errorf("expected System: line: %q", out)
	}
}

func TestChatWithAgent_ZeroUsage_NoTokenLine_ITER6(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	prov := &mockProvider{resp: &model.ChatResponse{
		Content: "hi",
		Usage:   model.Usage{},
	}}
	a, _ := agent.New("a1", "T").WithModel(prov).Build()
	r.SetAgent(a)

	out := captureStdout(t, func() {
		r.chatWithAgent("hello")
	})
	if strings.Contains(out, "[tokens:") {
		t.Errorf("did not expect token line, got %q", out)
	}
	if !strings.Contains(out, "hi") {
		t.Errorf("expected content: %q", out)
	}
}
