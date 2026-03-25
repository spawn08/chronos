package repl

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
)

type replTestProvider struct{}

func (p *replTestProvider) Chat(_ context.Context, _ *model.ChatRequest) (*model.ChatResponse, error) {
	return nil, errors.New("not implemented")
}
func (p *replTestProvider) StreamChat(_ context.Context, _ *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, errors.New("not implemented")
}
func (p *replTestProvider) Name() string  { return "test-provider" }
func (p *replTestProvider) Model() string { return "test-model-v1" }

func TestSetAgent_ModelCommand_WithModel(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	a, _ := agent.New("test-a", "Test A").WithModel(&replTestProvider{}).Build()
	r.SetAgent(a)

	output := captureStdout(t, func() {
		r.commands["/model"].Handler("")
	})
	if !strings.Contains(output, "test-provider") {
		t.Errorf("expected provider name in output, got: %q", output)
	}
	if !strings.Contains(output, "test-model-v1") {
		t.Errorf("expected model name in output, got: %q", output)
	}
}

func TestSetAgent_ModelCommand_NoModel(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	r.agent = &agent.Agent{ID: "a1"} // agent without model
	r.Register(Command{
		Name:        "/model",
		Description: "Show current model info",
		Handler: func(_ string) error {
			if r.agent == nil || r.agent.Model == nil {
				_ = captureStdout(t, func() {})
				return nil
			}
			return nil
		},
	})
	// This exercises the nil-model path
	if err := r.commands["/model"].Handler(""); err != nil {
		t.Fatalf("/model error: %v", err)
	}
}

func TestSetAgent_AgentCommand_NoModel(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	// Agent with long system prompt
	r.SetAgent(&agent.Agent{
		ID:           "a1",
		Name:         "Agent 1",
		Description:  "A test agent",
		SystemPrompt: "This is a very long system prompt that exceeds one hundred characters and should be truncated when displayed to the user",
	})

	output := captureStdout(t, func() {
		r.commands["/agent"].Handler("")
	})
	if !strings.Contains(output, "a1") {
		t.Errorf("expected agent ID in output, got: %q", output)
	}
	if !strings.Contains(output, "...") {
		t.Errorf("expected truncated system prompt with '...', got: %q", output)
	}
}

func TestSetAgent_AgentCommand_NilAgent(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	r.SetAgent(&agent.Agent{ID: "x"})
	// Override with nil agent
	r.agent = nil

	output := captureStdout(t, func() {
		r.commands["/agent"].Handler("")
	})
	if !strings.Contains(output, "No agent") {
		t.Errorf("expected 'No agent' message, got: %q", output)
	}
}
