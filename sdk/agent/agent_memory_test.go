package agent

import (
	"context"
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/knowledge"
	"github.com/spawn08/chronos/sdk/memory"
	"github.com/spawn08/chronos/sdk/skill"
)

func TestBuilder_WithMemory(t *testing.T) {
	store := newTestStorage()
	mem := memory.NewStore("a1", store)
	a, err := New("a1", "Test").
		WithModel(&testProvider{response: &model.ChatResponse{Content: "hi"}}).
		WithMemory(mem).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if a.Memory == nil {
		t.Error("expected Memory to be set")
	}
}

func TestBuilder_WithMemoryManager(t *testing.T) {
	store := newTestStorage()
	mem := memory.NewStore("a1", store)
	mgr := memory.NewManager("a1", "user1", mem, &testProvider{response: &model.ChatResponse{Content: "summary"}})
	a, err := New("a1", "Test").
		WithModel(&testProvider{response: &model.ChatResponse{Content: "hi"}}).
		WithMemoryManager(mgr).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if a.MemoryManager == nil {
		t.Error("expected MemoryManager to be set")
	}
}

func TestBuilder_AddSkill(t *testing.T) {
	s := &skill.Skill{
		Name:    "Test Skill",
		Version: "1.0.0",
	}
	a, err := New("a1", "Test").
		WithModel(&testProvider{response: &model.ChatResponse{Content: "hi"}}).
		AddSkill(s).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
}

// mockKnowledge implements knowledge.Knowledge for testing.
type mockKnowledge struct{}

func (m *mockKnowledge) Load(ctx context.Context) error { return nil }
func (m *mockKnowledge) Search(ctx context.Context, query string, limit int) ([]knowledge.Document, error) {
	return []knowledge.Document{{ID: "doc1", Content: "test content"}}, nil
}
func (m *mockKnowledge) Close() error { return nil }

func TestBuilder_WithKnowledge(t *testing.T) {
	k := &mockKnowledge{}
	a, err := New("a1", "Test").
		WithModel(&testProvider{response: &model.ChatResponse{Content: "hi"}}).
		WithKnowledge(k).
		Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if a.Knowledge == nil {
		t.Error("expected Knowledge to be set")
	}
}
