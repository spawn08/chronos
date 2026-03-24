package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/spawn08/chronos/engine/model"
)

// mockProvider is a simple model.Provider for testing the memory manager.
type mockProvider struct {
	response string
	err      error
}

func (p *mockProvider) Chat(_ context.Context, _ *model.ChatRequest) (*model.ChatResponse, error) {
	if p.err != nil {
		return nil, p.err
	}
	return &model.ChatResponse{Content: p.response}, nil
}

func (p *mockProvider) StreamChat(_ context.Context, _ *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, errors.New("not implemented")
}

func (p *mockProvider) Name() string  { return "mock" }
func (p *mockProvider) Model() string { return "mock-model" }

func TestManager_ExtractMemories_Success(t *testing.T) {
	backend := newMemStorage()
	store := NewStore("agent1", backend)
	provider := &mockProvider{
		response: `[{"key":"user_name","value":"Alice"},{"key":"favorite_color","value":"blue"}]`,
	}
	mgr := NewManager("agent1", "user1", store, provider)

	messages := []model.Message{
		{Role: "user", Content: "My name is Alice and I like blue."},
		{Role: "assistant", Content: "Nice to meet you, Alice!"},
	}
	if err := mgr.ExtractMemories(context.Background(), messages); err != nil {
		t.Fatalf("ExtractMemories: %v", err)
	}

	recs, err := store.ListLongTerm(context.Background())
	if err != nil {
		t.Fatalf("ListLongTerm: %v", err)
	}
	if len(recs) != 2 {
		t.Errorf("expected 2 memories, got %d", len(recs))
	}
}

func TestManager_ExtractMemories_ProviderError(t *testing.T) {
	backend := newMemStorage()
	store := NewStore("agent1", backend)
	provider := &mockProvider{err: errors.New("provider down")}
	mgr := NewManager("agent1", "user1", store, provider)

	err := mgr.ExtractMemories(context.Background(), []model.Message{
		{Role: "user", Content: "hello"},
	})
	if err == nil {
		t.Fatal("expected error from provider")
	}
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

func TestManager_ExtractMemories_InvalidJSON(t *testing.T) {
	// When model returns invalid JSON, should not error — just skip
	backend := newMemStorage()
	store := NewStore("agent1", backend)
	provider := &mockProvider{response: "this is not json"}
	mgr := NewManager("agent1", "user1", store, provider)

	if err := mgr.ExtractMemories(context.Background(), []model.Message{
		{Role: "user", Content: "hello"},
	}); err != nil {
		t.Errorf("should not error on invalid JSON: %v", err)
	}
}

func TestManager_ExtractMemories_EmptyArray(t *testing.T) {
	backend := newMemStorage()
	store := NewStore("agent1", backend)
	provider := &mockProvider{response: "[]"}
	mgr := NewManager("agent1", "user1", store, provider)

	if err := mgr.ExtractMemories(context.Background(), []model.Message{
		{Role: "user", Content: "nothing memorable"},
	}); err != nil {
		t.Fatalf("ExtractMemories: %v", err)
	}

	recs, _ := store.ListLongTerm(context.Background())
	if len(recs) != 0 {
		t.Errorf("expected 0 memories, got %d", len(recs))
	}
}

func TestManager_OptimizeMemories_TooFew(t *testing.T) {
	// With fewer than 5 memories, OptimizeMemories should be a no-op
	backend := newMemStorage()
	store := NewStore("agent1", backend)
	provider := &mockProvider{response: "[]"}
	mgr := NewManager("agent1", "user1", store, provider)

	ctx := context.Background()
	_ = store.SetLongTerm(ctx, "k1", "v1")
	_ = store.SetLongTerm(ctx, "k2", "v2")

	if err := mgr.OptimizeMemories(ctx); err != nil {
		t.Fatalf("OptimizeMemories: %v", err)
	}

	// Records should be unchanged
	recs, _ := store.ListLongTerm(ctx)
	if len(recs) != 2 {
		t.Errorf("expected 2 memories unchanged, got %d", len(recs))
	}
}

func TestManager_OptimizeMemories_Success(t *testing.T) {
	backend := newMemStorage()
	store := NewStore("agent1", backend)
	provider := &mockProvider{response: `[{"key":"merged","value":"combined fact"}]`}
	mgr := NewManager("agent1", "user1", store, provider)

	ctx := context.Background()
	// Add 5+ memories to trigger optimization
	for i := 0; i < 6; i++ {
		store.SetLongTerm(ctx, "key"+string(rune('0'+i)), "value")
	}

	if err := mgr.OptimizeMemories(ctx); err != nil {
		t.Fatalf("OptimizeMemories: %v", err)
	}

	recs, _ := store.ListLongTerm(ctx)
	if len(recs) != 1 {
		t.Errorf("expected 1 optimized memory, got %d", len(recs))
	}
}

func TestManager_OptimizeMemories_InvalidJSON(t *testing.T) {
	backend := newMemStorage()
	store := NewStore("agent1", backend)
	provider := &mockProvider{response: "not json"}
	mgr := NewManager("agent1", "user1", store, provider)

	ctx := context.Background()
	for i := 0; i < 6; i++ {
		store.SetLongTerm(ctx, "key"+string(rune('a'+i)), "value")
	}

	// Should not error — just skip on bad JSON
	if err := mgr.OptimizeMemories(ctx); err != nil {
		t.Fatalf("OptimizeMemories should not error on invalid JSON: %v", err)
	}
}

func TestManager_GetUserMemories_Empty(t *testing.T) {
	backend := newMemStorage()
	store := NewStore("agent1", backend)
	provider := &mockProvider{}
	mgr := NewManager("agent1", "user1", store, provider)

	result, err := mgr.GetUserMemories(context.Background())
	if err != nil {
		t.Fatalf("GetUserMemories: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string for no memories, got %q", result)
	}
}

func TestManager_GetUserMemories_WithMemories(t *testing.T) {
	backend := newMemStorage()
	store := NewStore("agent1", backend)
	provider := &mockProvider{}
	mgr := NewManager("agent1", "user1", store, provider)

	ctx := context.Background()
	store.SetLongTerm(ctx, "user_name", "Alice")
	store.SetLongTerm(ctx, "preference", "dark mode")

	result, err := mgr.GetUserMemories(ctx)
	if err != nil {
		t.Fatalf("GetUserMemories: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
	if !contains(result, "User memories:") {
		t.Errorf("expected 'User memories:' in result: %q", result)
	}
}

func TestManager_MemoryTools_Remember(t *testing.T) {
	backend := newMemStorage()
	store := NewStore("agent1", backend)
	provider := &mockProvider{}
	mgr := NewManager("agent1", "user1", store, provider)

	tools := mgr.MemoryTools()
	if len(tools) != 3 {
		t.Fatalf("expected 3 memory tools, got %d", len(tools))
	}

	// Find remember tool
	var rememberTool *MemoryTool
	for i := range tools {
		if tools[i].Name == "remember" {
			rememberTool = &tools[i]
		}
	}
	if rememberTool == nil {
		t.Fatal("remember tool not found")
	}

	_, err := rememberTool.Handler(context.Background(), map[string]any{"key": "test_key", "value": "test_value"})
	if err != nil {
		t.Fatalf("remember handler: %v", err)
	}

	recs, _ := store.ListLongTerm(context.Background())
	if len(recs) != 1 {
		t.Errorf("expected 1 memory after remember, got %d", len(recs))
	}
}

func TestManager_MemoryTools_Remember_MissingKey(t *testing.T) {
	backend := newMemStorage()
	store := NewStore("agent1", backend)
	mgr := NewManager("agent1", "user1", store, &mockProvider{})

	tools := mgr.MemoryTools()
	var rememberTool *MemoryTool
	for i := range tools {
		if tools[i].Name == "remember" {
			rememberTool = &tools[i]
		}
	}

	_, err := rememberTool.Handler(context.Background(), map[string]any{"value": "no key"})
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestManager_MemoryTools_Forget(t *testing.T) {
	backend := newMemStorage()
	store := NewStore("agent1", backend)
	mgr := NewManager("agent1", "user1", store, &mockProvider{})

	ctx := context.Background()
	store.SetLongTerm(ctx, "test_key", "test_value")

	tools := mgr.MemoryTools()
	var forgetTool *MemoryTool
	for i := range tools {
		if tools[i].Name == "forget" {
			forgetTool = &tools[i]
		}
	}
	if forgetTool == nil {
		t.Fatal("forget tool not found")
	}

	_, err := forgetTool.Handler(ctx, map[string]any{"key": "test_key"})
	if err != nil {
		t.Fatalf("forget handler: %v", err)
	}

	recs, _ := store.ListLongTerm(ctx)
	if len(recs) != 0 {
		t.Errorf("expected 0 memories after forget, got %d", len(recs))
	}
}

func TestManager_MemoryTools_Recall(t *testing.T) {
	backend := newMemStorage()
	store := NewStore("agent1", backend)
	mgr := NewManager("agent1", "user1", store, &mockProvider{})

	ctx := context.Background()
	store.SetLongTerm(ctx, "k1", "v1")
	store.SetLongTerm(ctx, "k2", "v2")

	tools := mgr.MemoryTools()
	var recallTool *MemoryTool
	for i := range tools {
		if tools[i].Name == "recall" {
			recallTool = &tools[i]
		}
	}
	if recallTool == nil {
		t.Fatal("recall tool not found")
	}

	result, err := recallTool.Handler(ctx, nil)
	if err != nil {
		t.Fatalf("recall handler: %v", err)
	}
	items, _ := result.([]map[string]any)
	if len(items) != 2 {
		t.Errorf("expected 2 recall results, got %d", len(items))
	}
}

// contains is a helper for string check.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
