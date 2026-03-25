package graph

import (
	"context"
	"strings"
	"testing"
)

func noopNode(_ context.Context, state State) (State, error) {
	return state, nil
}

func TestCompile_NoEntryPoint_Push(t *testing.T) {
	g := New("g1")
	g.AddNode("a", noopNode)
	_, err := g.Compile()
	if err == nil || !strings.Contains(err.Error(), "no entry point") {
		t.Fatalf("expected no entry point error, got %v", err)
	}
}

func TestCompile_EntryNodeMissing_Push(t *testing.T) {
	g := New("g2")
	g.edges = append(g.edges, &Edge{From: StartNode, To: "ghost"})
	_, err := g.Compile()
	if err == nil || !strings.Contains(err.Error(), "entry node") {
		t.Fatalf("expected entry node error, got %v", err)
	}
}

func TestCompile_EdgeTargetMissing_Push(t *testing.T) {
	g := New("g3")
	g.AddNode("a", noopNode)
	g.SetEntryPoint("a")
	g.AddEdge("a", "missing-node")
	_, err := g.Compile()
	if err == nil || !strings.Contains(err.Error(), "edge target") {
		t.Fatalf("expected edge target error, got %v", err)
	}
}
