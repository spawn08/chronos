package graph

import (
	"context"
	"errors"
	"testing"
)

func TestMultipleFinishPoints(t *testing.T) {
	g := New("multi-finish")
	g.AddNode("a", func(_ context.Context, s State) (State, error) { return s, nil })
	g.AddNode("b", func(_ context.Context, s State) (State, error) { return s, nil })
	g.AddNode("c", func(_ context.Context, s State) (State, error) { return s, nil })
	g.SetEntryPoint("a")
	g.AddEdge("a", "b")
	g.AddEdge("a", "c")
	g.SetFinishPoint("b")
	g.SetFinishPoint("c")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if compiled == nil {
		t.Fatal("expected compiled graph")
	}
}

func TestCompile_MultipleEdgesFromNode(t *testing.T) {
	g := New("fork")
	g.AddNode("start", func(_ context.Context, s State) (State, error) { return s, nil })
	g.AddNode("left", func(_ context.Context, s State) (State, error) { return s, nil })
	g.AddNode("right", func(_ context.Context, s State) (State, error) { return s, nil })
	g.SetEntryPoint("start")
	g.AddEdge("start", "left")
	g.AddEdge("start", "right")
	g.SetFinishPoint("left")
	g.SetFinishPoint("right")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	edges := compiled.AdjList["start"]
	if len(edges) != 2 {
		t.Errorf("expected 2 edges from start, got %d", len(edges))
	}
}

func TestStateGraph_AddConditionalEdge_NoStaticTo(t *testing.T) {
	g := New("cond")
	g.AddNode("router", func(_ context.Context, s State) (State, error) { return s, nil })
	g.AddNode("target", func(_ context.Context, s State) (State, error) { return s, nil })
	g.SetEntryPoint("router")
	g.AddConditionalEdge("router", func(_ State) string { return "target" })
	g.SetFinishPoint("target")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	edges := compiled.AdjList["router"]
	if len(edges) != 1 || edges[0].Condition == nil {
		t.Errorf("expected conditional edge from router, got: %v", edges)
	}
}

func TestStateGraph_GraphID(t *testing.T) {
	g := New("my-graph-id")
	g.AddNode("n", func(_ context.Context, s State) (State, error) { return s, nil })
	g.SetEntryPoint("n")
	g.SetFinishPoint("n")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if compiled.ID != "my-graph-id" {
		t.Errorf("ID=%q, want my-graph-id", compiled.ID)
	}
}

func TestStateGraph_NodeCount(t *testing.T) {
	g := New("count")
	for _, name := range []string{"n1", "n2", "n3", "n4", "n5"} {
		n := name
		g.AddNode(n, func(_ context.Context, s State) (State, error) { return s, nil })
	}
	g.SetEntryPoint("n1")
	g.AddEdge("n1", "n2")
	g.AddEdge("n2", "n3")
	g.AddEdge("n3", "n4")
	g.AddEdge("n4", "n5")
	g.SetFinishPoint("n5")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(compiled.Nodes) != 5 {
		t.Errorf("expected 5 nodes, got %d", len(compiled.Nodes))
	}
}

func TestStateGraph_EdgeFromMissingNode(t *testing.T) {
	g := New("bad-from")
	g.AddNode("existing", func(_ context.Context, s State) (State, error) { return s, nil })
	g.SetEntryPoint("existing")
	// Add edge from nonexistent node — compile should detect missing target
	g.AddEdge("existing", "missing")

	_, err := g.Compile()
	if err == nil {
		t.Fatal("expected error for edge to missing node")
	}
}

func TestStateGraph_InterruptAndNormalMix(t *testing.T) {
	g := New("mixed")
	g.AddNode("step1", func(_ context.Context, s State) (State, error) { return s, nil })
	g.AddInterruptNode("approval", func(_ context.Context, s State) (State, error) { return s, nil })
	g.AddNode("step2", func(_ context.Context, s State) (State, error) { return s, nil })
	g.SetEntryPoint("step1")
	g.AddEdge("step1", "approval")
	g.AddEdge("approval", "step2")
	g.SetFinishPoint("step2")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	approvalNode := compiled.Nodes["approval"]
	if approvalNode == nil {
		t.Fatal("approval node not found")
	}
	if !approvalNode.Interrupt {
		t.Error("approval node should be interrupt")
	}

	step1Node := compiled.Nodes["step1"]
	if step1Node.Interrupt {
		t.Error("step1 node should not be interrupt")
	}
}

func TestNodeFunc_ReceivesState(t *testing.T) {
	g := New("state-test")
	var receivedState State
	g.AddNode("capture", func(_ context.Context, s State) (State, error) {
		receivedState = s
		return s, nil
	})
	g.SetEntryPoint("capture")
	g.SetFinishPoint("capture")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	// Execute the node directly via its stored function
	inputState := State{"key": "value", "num": 42}
	fn := compiled.Nodes["capture"].Fn
	out, err := fn(context.Background(), inputState)
	if err != nil {
		t.Fatalf("node func: %v", err)
	}
	if receivedState["key"] != "value" {
		t.Errorf("received state missing key: %v", receivedState)
	}
	if out["key"] != "value" {
		t.Errorf("output state missing key: %v", out)
	}
}

func TestNodeFunc_PropagatesError(t *testing.T) {
	g := New("error-test")
	g.AddNode("fail", func(_ context.Context, _ State) (State, error) {
		return nil, errors.New("failure")
	})
	g.SetEntryPoint("fail")
	g.SetFinishPoint("fail")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	fn := compiled.Nodes["fail"].Fn
	_, err = fn(context.Background(), State{})
	if err == nil {
		t.Fatal("expected error from failing node func")
	}
	if err.Error() != "failure" {
		t.Errorf("error=%q, want failure", err.Error())
	}
}

func TestAdjList_ConditionalAndStatic(t *testing.T) {
	g := New("mixed-edges")
	g.AddNode("hub", func(_ context.Context, s State) (State, error) { return s, nil })
	g.AddNode("static_target", func(_ context.Context, s State) (State, error) { return s, nil })
	g.AddNode("cond_target", func(_ context.Context, s State) (State, error) { return s, nil })

	g.SetEntryPoint("hub")
	g.AddEdge("hub", "static_target")
	g.AddConditionalEdge("hub", func(s State) string { return "cond_target" })
	g.SetFinishPoint("static_target")
	g.SetFinishPoint("cond_target")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	edges := compiled.AdjList["hub"]
	// 1 static + 1 conditional
	if len(edges) != 2 {
		t.Errorf("expected 2 edges from hub, got %d", len(edges))
	}
	var hasStatic, hasCond bool
	for _, e := range edges {
		if e.Condition != nil {
			hasCond = true
		} else {
			hasStatic = true
		}
	}
	if !hasStatic {
		t.Error("expected static edge from hub")
	}
	if !hasCond {
		t.Error("expected conditional edge from hub")
	}
}

func TestConditionalEdge_FunctionIsCallable(t *testing.T) {
	g := New("callable-cond")
	g.AddNode("src", func(_ context.Context, s State) (State, error) { return s, nil })
	g.AddNode("dest", func(_ context.Context, s State) (State, error) { return s, nil })
	g.SetEntryPoint("src")
	g.AddConditionalEdge("src", func(s State) string {
		return "dest"
	})
	g.SetFinishPoint("dest")

	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	edges := compiled.AdjList["src"]
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	result := edges[0].Condition(State{"any": "value"})
	if result != "dest" {
		t.Errorf("condition returned %q, want dest", result)
	}
}
