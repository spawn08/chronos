package main

import (
	"context"
	"testing"
)

// These tests are fully offline: they exercise the pure tool helpers and the
// tool registry wiring without touching Azure or the network.

func TestCalculate(t *testing.T) {
	tests := []struct {
		name    string
		a, b    float64
		op      string
		want    float64
		wantErr bool
	}{
		{name: "add", a: 2, b: 3, op: "add", want: 5},
		{name: "sub", a: 10, b: 4, op: "sub", want: 6},
		{name: "mul", a: 6, b: 7, op: "mul", want: 42},
		{name: "div", a: 9, b: 3, op: "div", want: 3},
		{name: "div by zero", a: 1, b: 0, op: "div", wantErr: true},
		{name: "unknown op", a: 1, b: 1, op: "pow", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calculate(tt.a, tt.b, tt.op)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got["result"].(float64) != tt.want {
				t.Errorf("calculate(%v,%v,%q) = %v, want %v", tt.a, tt.b, tt.op, got["result"], tt.want)
			}
		})
	}
}

func TestLookup(t *testing.T) {
	tests := []struct {
		name      string
		entity    string
		wantFound bool
	}{
		{name: "france", entity: "France population", wantFound: true},
		{name: "lowercase japan", entity: "japan", wantFound: true},
		{name: "unknown", entity: "Atlantis population", wantFound: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lookup(tt.entity)
			if got["found"].(bool) != tt.wantFound {
				t.Errorf("lookup(%q).found = %v, want %v", tt.entity, got["found"], tt.wantFound)
			}
		})
	}
}

func TestRegistryExecute(t *testing.T) {
	registry := newToolRegistry()
	if len(registry.List()) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(registry.List()))
	}

	res, err := registry.Execute(context.Background(), "calculator", map[string]any{
		"a": 4.0, "b": 5.0, "op": "mul",
	})
	if err != nil {
		t.Fatalf("execute calculator: %v", err)
	}
	m := res.(map[string]any)
	if m["result"].(float64) != 20 {
		t.Errorf("calculator via registry = %v, want 20", m["result"])
	}
}
