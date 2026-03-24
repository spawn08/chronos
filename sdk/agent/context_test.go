package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/storage/adapters/memory"
)

func TestEvictLargeResult_SmallResult(t *testing.T) {
	store := memory.New()
	ctx := context.Background()
	result, err := EvictLargeResult(ctx, store, "sess1", "tool1", "small")
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("small results should not be evicted")
	}
}

func TestEvictLargeResult_LargeResult(t *testing.T) {
	store := memory.New()
	ctx := context.Background()

	largeData := strings.Repeat("x", 2000)
	result, err := EvictLargeResult(ctx, store, "sess1", "tool1", largeData)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("large result should be evicted")
	}
	if result.StorageKey == "" {
		t.Error("storage key should not be empty")
	}
	if result.FullSize != len(`"`+largeData+`"`) {
		t.Errorf("full size mismatch: got %d", result.FullSize)
	}

	retrieved, err := ReadStoredResult(ctx, store, "sess1", result.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(retrieved, "xxx") {
		t.Error("retrieved result should contain original data")
	}
}

func TestReadStoredResult_NotFound(t *testing.T) {
	store := memory.New()
	_, err := ReadStoredResult(context.Background(), store, "sess1", "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent key")
	}
}

func TestCompressToolCalls_BelowLimit(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleUser, Content: "hello"},
		{Role: model.RoleAssistant, Content: "hi", ToolCalls: []model.ToolCall{{ID: "1", Name: "search"}}},
		{Role: model.RoleTool, Content: "result", ToolCallID: "1"},
	}
	result := CompressToolCalls(messages, 5)
	if len(result) != 3 {
		t.Errorf("got %d messages, want 3 (under limit)", len(result))
	}
}

func TestCompressToolCalls_AboveLimit(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleSystem, Content: "system prompt"},
		{Role: model.RoleUser, Content: "q1"},
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "1", Name: "t1"}}},
		{Role: model.RoleTool, Content: "r1", ToolCallID: "1"},
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "2", Name: "t2"}}},
		{Role: model.RoleTool, Content: "r2", ToolCallID: "2"},
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "3", Name: "t3"}}},
		{Role: model.RoleTool, Content: "r3", ToolCallID: "3"},
		{Role: model.RoleUser, Content: "q2"},
	}

	result := CompressToolCalls(messages, 1)

	hasSystem := false
	hasQ1 := false
	hasQ2 := false
	for _, m := range result {
		if m.Role == model.RoleSystem {
			hasSystem = true
		}
		if m.Content == "q1" {
			hasQ1 = true
		}
		if m.Content == "q2" {
			hasQ2 = true
		}
	}
	if !hasSystem {
		t.Error("system message should be preserved")
	}
	if !hasQ1 {
		t.Error("user message q1 should be preserved")
	}
	if !hasQ2 {
		t.Error("user message q2 should be preserved")
	}
	if len(result) >= len(messages) {
		t.Errorf("compressed should have fewer messages: %d >= %d", len(result), len(messages))
	}
}

func TestCompressToolCalls_EmptyMessages(t *testing.T) {
	result := CompressToolCalls(nil, 5)
	if result != nil {
		t.Error("nil input should return nil")
	}
}

func TestCompressToolCalls_ZeroLimit(t *testing.T) {
	messages := []model.Message{{Role: model.RoleUser, Content: "test"}}
	result := CompressToolCalls(messages, 0)
	if len(result) != 1 {
		t.Error("zero limit should return original")
	}
}
