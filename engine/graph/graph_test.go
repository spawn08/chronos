package graph

import (
	"context"
	"testing"
)

func TestNew(t *testing.T) {
	g := New("test-id")
	if g.id != "test-id" {
		t.Errorf("id = %q, want test-id", g.id)
	}
	if len(g.nodes) != 0 {
		t.Error("new graph should have 0 nodes")
	}
}

func TestAddNode(t *testing.T) {
	g := New("g")
	g.AddNode("step1", func(_ context.Context, s State) (State, error) {
		return s, nil
	})
	if _, ok := g.nodes["step1"]; !ok {
		t.Error("step1 should exist in nodes")
	}
	if g.nodes["step1"].Interrupt {
		t.Error("normal node should not be interrupt")
	}
}

func TestAddInterruptNode(t *testing.T) {
	g := New("g")
	g.AddInterruptNode("approval", func(_ context.Context, s State) (State, error) {
		return s, nil
	})
	if !g.nodes["approval"].Interrupt {
		t.Error("interrupt node should have Interrupt=true")
	}
}

func TestSetEntryAndFinishPoint(t *testing.T) {
	g := New("g")
	g.AddNode("start", func(_ context.Context, s State) (State, error) { return s, nil })
	g.SetEntryPoint("start")
	g.SetFinishPoint("start")

	hasStart := false
	hasEnd := false
	for _, e := range g.edges {
		if e.From == StartNode && e.To == "start" {
			hasStart = true
		}
		if e.From == "start" && e.To == EndNode {
			hasEnd = true
		}
	}
	if !hasStart {
		t.Error("expected __start__ -> start edge")
	}
	if !hasEnd {
		t.Error("expected start -> __end__ edge")
	}
}

func TestCompile_Success(t *testing.T) {
	g := New("simple")
	g.AddNode("a", func(_ context.Context, s State) (State, error) { return s, nil })
	g.AddNode("b", func(_ context.Context, s State) (State, error) { return s, nil })
	g.SetEntryPoint("a")
	g.AddEdge("a", "b")
	g.SetFinishPoint("b")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if compiled.ID != "simple" {
		t.Errorf("ID = %q", compiled.ID)
	}
	if compiled.Entry != "a" {
		t.Errorf("Entry = %q, want a", compiled.Entry)
	}
	if len(compiled.Nodes) != 2 {
		t.Errorf("Nodes count = %d, want 2", len(compiled.Nodes))
	}
}

func TestCompile_NoEntryPoint(t *testing.T) {
	g := New("no-entry")
	g.AddNode("a", func(_ context.Context, s State) (State, error) { return s, nil })

	_, err := g.Compile()
	if err == nil {
		t.Fatal("expected error for missing entry point")
	}
}

func TestCompile_EntryNodeNotFound(t *testing.T) {
	g := New("bad-entry")
	g.SetEntryPoint("nonexistent")

	_, err := g.Compile()
	if err == nil {
		t.Fatal("expected error for missing entry node")
	}
}

func TestCompile_EdgeTargetNotFound(t *testing.T) {
	g := New("bad-target")
	g.AddNode("a", func(_ context.Context, s State) (State, error) { return s, nil })
	g.SetEntryPoint("a")
	g.AddEdge("a", "missing_node")

	_, err := g.Compile()
	if err == nil {
		t.Fatal("expected error for missing edge target")
	}
}

func TestCompile_ConditionalEdge(t *testing.T) {
	g := New("cond")
	g.AddNode("router", func(_ context.Context, s State) (State, error) { return s, nil })
	g.AddNode("left", func(_ context.Context, s State) (State, error) { return s, nil })
	g.AddNode("right", func(_ context.Context, s State) (State, error) { return s, nil })
	g.SetEntryPoint("router")
	g.AddConditionalEdge("router", func(s State) string {
		if s["dir"] == "left" {
			return "left"
		}
		return "right"
	})
	g.SetFinishPoint("left")
	g.SetFinishPoint("right")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	edges := compiled.AdjList["router"]
	if len(edges) != 1 {
		t.Fatalf("expected 1 conditional edge from router, got %d", len(edges))
	}
	if edges[0].Condition == nil {
		t.Error("edge should have a Condition")
	}
}

func TestAddEdge_Chaining(t *testing.T) {
	g := New("chain")
	result := g.AddNode("a", func(_ context.Context, s State) (State, error) { return s, nil }).
		AddNode("b", func(_ context.Context, s State) (State, error) { return s, nil }).
		SetEntryPoint("a").
		AddEdge("a", "b").
		SetFinishPoint("b")

	if result != g {
		t.Error("chaining should return the same graph")
	}
}
