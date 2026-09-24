package tool

import (
	"context"
	"strings"
	"testing"
)

func TestRegistryEnforcesDeclaredEffects(t *testing.T) {
	registry := NewRegistry()
	executions := 0
	registry.Register(&Definition{Name: "read", Effects: []Effect{EffectRead}, Handler: func(context.Context, map[string]any) (any, error) {
		executions++
		return nil, nil
	}})
	registry.Register(&Definition{Name: "write", Effects: []Effect{EffectDeliveryWrite}, Handler: func(context.Context, map[string]any) (any, error) {
		executions++
		return nil, nil
	}})
	registry.Register(&Definition{Name: "undeclared", Handler: func(context.Context, map[string]any) (any, error) {
		executions++
		return nil, nil
	}})

	ctx := WithEffectGrant(context.Background(), EffectRead)
	if _, err := registry.Execute(ctx, "read", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(ctx, "write", nil); err == nil || !strings.Contains(err.Error(), "undelegated effect") {
		t.Fatalf("write error = %v", err)
	}
	if _, err := registry.Execute(ctx, "undeclared", nil); err == nil || !strings.Contains(err.Error(), "no declared effects") {
		t.Fatalf("undeclared error = %v", err)
	}
	if executions != 1 {
		t.Fatalf("executions = %d, want 1", executions)
	}
}

func TestRegistryResolvesScratchWriteEffect(t *testing.T) {
	registry := NewRegistry()
	registry.Register(&Definition{
		Name: "write", Effects: []Effect{EffectScratchWrite, EffectDeliveryWrite},
		ResolveEffects: func(ctx context.Context, _ map[string]any) ([]Effect, error) {
			if IsScratchWorkspace(ctx) {
				return []Effect{EffectScratchWrite}, nil
			}
			return []Effect{EffectDeliveryWrite}, nil
		},
		Handler: func(context.Context, map[string]any) (any, error) { return nil, nil },
	})
	scratchOnly := WithEffectGrant(WithScratchWorkspace(context.Background()), EffectScratchWrite)
	if _, err := registry.Execute(scratchOnly, "write", nil); err != nil {
		t.Fatalf("scratch write: %v", err)
	}
	delivery := WithEffectGrant(context.Background(), EffectScratchWrite)
	if _, err := registry.Execute(delivery, "write", nil); err == nil {
		t.Fatal("scratch grant wrote to delivery workspace")
	}
}
