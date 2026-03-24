package graph

import (
	"context"
	"strings"
	"testing"
)

func TestSubgraphNode_MissingNodeError(t *testing.T) {
	sub := &CompiledGraph{
		ID:    "bad",
		Entry: "ghost",
		Nodes: map[string]*Node{},
		AdjList: map[string][]*Edge{
			"ghost": {},
		},
	}
	_, err := SubgraphNode(sub)(context.Background(), State{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("got %v", err)
	}
}

func TestSubgraphNode_NodeFnError(t *testing.T) {
	sub := &CompiledGraph{
		ID:    "errg",
		Entry: "boom",
		Nodes: map[string]*Node{
			"boom": {
				ID: "boom",
				Fn: func(context.Context, State) (State, error) {
					return nil, context.DeadlineExceeded
				},
			},
		},
		AdjList: map[string][]*Edge{"boom": {{To: EndNode}}},
	}
	_, err := SubgraphNode(sub)(context.Background(), State{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("got %v", err)
	}
}

func TestFindSubgraphNext_NoEdgesReturnsEmpty(t *testing.T) {
	g := &CompiledGraph{
		AdjList: map[string][]*Edge{"x": {}},
	}
	if got := findSubgraphNext(g, "x", State{}); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestFindSubgraphNext_StaticEdge(t *testing.T) {
	g := &CompiledGraph{
		AdjList: map[string][]*Edge{
			"a": {{To: "b"}},
		},
	}
	if findSubgraphNext(g, "a", State{}) != "b" {
		t.Fatal("expected static To")
	}
}
