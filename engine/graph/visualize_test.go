package graph

import (
	"context"
	"strings"
	"testing"
)

func TestToMermaid_LinearGraph(t *testing.T) {
	g := New("test")
	g.AddNode("a", func(_ context.Context, s State) (State, error) { return s, nil })
	g.AddNode("b", func(_ context.Context, s State) (State, error) { return s, nil })
	g.SetEntryPoint("a")
	g.AddEdge("a", "b")
	g.SetFinishPoint("b")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatal(err)
	}

	mermaid := compiled.ToMermaid()
	if !strings.Contains(mermaid, "flowchart TD") {
		t.Error("missing flowchart header")
	}
	if !strings.Contains(mermaid, "a[a]") {
		t.Error("missing node a")
	}
	if !strings.Contains(mermaid, "__start__") {
		t.Error("missing start node")
	}
	if !strings.Contains(mermaid, "__end__") {
		t.Error("missing end node")
	}
}

func TestToMermaid_InterruptNode(t *testing.T) {
	g := New("intr")
	g.AddNode("step", func(_ context.Context, s State) (State, error) { return s, nil })
	g.AddInterruptNode("approval", func(_ context.Context, s State) (State, error) { return s, nil })
	g.SetEntryPoint("step")
	g.AddEdge("step", "approval")
	g.SetFinishPoint("approval")

	compiled, _ := g.Compile()
	mermaid := compiled.ToMermaid()

	if !strings.Contains(mermaid, "approval{{approval}}") {
		t.Error("interrupt node should use {{}} notation")
	}
}

func TestToDOT_LinearGraph(t *testing.T) {
	g := New("test-graph")
	g.AddNode("a", func(_ context.Context, s State) (State, error) { return s, nil })
	g.AddNode("b", func(_ context.Context, s State) (State, error) { return s, nil })
	g.SetEntryPoint("a")
	g.AddEdge("a", "b")
	g.SetFinishPoint("b")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatal(err)
	}

	dot := compiled.ToDOT()
	if !strings.Contains(dot, "digraph test_graph") {
		t.Error("missing digraph header")
	}
	if !strings.Contains(dot, "__start__") {
		t.Error("missing start node")
	}
	if !strings.Contains(dot, "__end__") {
		t.Error("missing end node")
	}
	if !strings.Contains(dot, "a -> b") {
		t.Error("missing edge a -> b")
	}
}

func TestToDOT_InterruptNode(t *testing.T) {
	g := New("intr")
	g.AddInterruptNode("human", func(_ context.Context, s State) (State, error) { return s, nil })
	g.SetEntryPoint("human")
	g.SetFinishPoint("human")

	compiled, _ := g.Compile()
	dot := compiled.ToDOT()

	if !strings.Contains(dot, "shape=diamond") {
		t.Error("interrupt node should use diamond shape")
	}
	if !strings.Contains(dot, "#FF9800") {
		t.Error("interrupt node should be orange")
	}
}

func TestToMermaid_ConditionalEdge(t *testing.T) {
	g := New("cond")
	g.AddNode("a", func(_ context.Context, s State) (State, error) { return s, nil })
	g.AddNode("b", func(_ context.Context, s State) (State, error) { return s, nil })
	g.SetEntryPoint("a")
	g.AddConditionalEdge("a", func(s State) string { return "b" })
	g.SetFinishPoint("b")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	mermaid := compiled.ToMermaid()
	if !strings.Contains(mermaid, "flowchart TD") {
		t.Error("missing flowchart header")
	}
	// Conditional edge should produce a conditional node notation
	if !strings.Contains(mermaid, "a") {
		t.Error("missing source node")
	}
}

func TestToDOT_ConditionalEdge(t *testing.T) {
	g := New("cond-dot")
	g.AddNode("step1", func(_ context.Context, s State) (State, error) { return s, nil })
	g.AddNode("step2", func(_ context.Context, s State) (State, error) { return s, nil })
	g.SetEntryPoint("step1")
	g.AddConditionalEdge("step1", func(s State) string { return "step2" })
	g.SetFinishPoint("step2")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	dot := compiled.ToDOT()
	if !strings.Contains(dot, "digraph") {
		t.Error("missing digraph header")
	}
	if !strings.Contains(dot, "conditional") {
		t.Error("conditional edge should have 'conditional' label")
	}
}

func TestSanitizeDOTID(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"hello", "hello"},
		{"my-node", "my_node"},
		{"my node", "my_node"},
		{"v1.0", "v1_0"},
	}
	for _, tt := range tests {
		got := sanitizeDOTID(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeDOTID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
