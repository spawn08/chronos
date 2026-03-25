package repl

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
)

// mockProvider is a minimal model.Provider for tests.
type mockProvider struct {
	resp *model.ChatResponse
	err  error
}

func (m *mockProvider) Chat(_ context.Context, _ *model.ChatRequest) (*model.ChatResponse, error) {
	return m.resp, m.err
}

func (m *mockProvider) StreamChat(_ context.Context, _ *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *mockProvider) Name() string  { return "mock" }
func (m *mockProvider) Model() string { return "mock-model" }

func TestChatWithAgent_Success(t *testing.T) {
	store := newTestStore(t)
	r := New(store)

	prov := &mockProvider{resp: &model.ChatResponse{
		Content: "Hello from mock!",
		Usage:   model.Usage{PromptTokens: 5, CompletionTokens: 10},
	}}
	a, _ := agent.New("a1", "Test").WithModel(prov).Build()
	r.SetAgent(a)

	output := captureStdout(t, func() {
		r.chatWithAgent("hi")
	})
	if !strings.Contains(output, "Hello from mock!") {
		t.Errorf("expected response in output, got: %q", output)
	}
	if !strings.Contains(output, "tokens") {
		t.Errorf("expected token info in output, got: %q", output)
	}
}

func TestChatWithAgent_Error(t *testing.T) {
	store := newTestStore(t)
	r := New(store)

	prov := &mockProvider{err: errors.New("model failure")}
	a, _ := agent.New("a1", "Test").WithModel(prov).Build()
	r.SetAgent(a)

	// Should not panic; error goes to stderr
	r.chatWithAgent("fail please")
}

func TestExecShell_EmptyString(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	// Should be a no-op, no panic
	r.execShell("")
}

func TestExecShell_ValidCommand(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	// Run a command that always succeeds
	r.execShell("echo hello")
}

func TestExecShell_InvalidCommand(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	// Should handle error gracefully
	r.execShell("nonexistent-binary-xyz-123")
}
