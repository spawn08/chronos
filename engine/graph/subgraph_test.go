package graph

import (
	"context"
	"testing"
)

func TestSubgraphNode_ExecutesInnerGraph(t *testing.T) {
	inner := New("inner").
		AddNode("double", func(_ context.Context, s State) (State, error) {
			v, _ := s["count"].(int)
			s["count"] = v * 2
			return s, nil
		}).
		AddNode("add_one", func(_ context.Context, s State) (State, error) {
			v, _ := s["count"].(int)
			s["count"] = v + 1
			return s, nil
		})
	inner.SetEntryPoint("double")
	inner.AddEdge("double", "add_one")
	inner.SetFinishPoint("add_one")

	compiled, err := inner.Compile()
	if err != nil {
		t.Fatalf("compile inner: %v", err)
	}

	fn := SubgraphNode(compiled)
	state := State{"count": 5}
	result, err := fn(context.Background(), state)
	if err != nil {
		t.Fatalf("subgraph: %v", err)
	}
	if got := result["count"]; got != 11 {
		t.Errorf("got count=%v, want 11 (5*2+1)", got)
	}
}

func TestAddSubgraph_IntegratesWithParent(t *testing.T) {
	inner := New("inner").
		AddNode("inc", func(_ context.Context, s State) (State, error) {
			v, _ := s["val"].(int)
			s["val"] = v + 10
			return s, nil
		})
	inner.SetEntryPoint("inc")
	inner.SetFinishPoint("inc")
	innerCompiled, err := inner.Compile()
	if err != nil {
		t.Fatal(err)
	}

	outer := New("outer").
		AddNode("setup", func(_ context.Context, s State) (State, error) {
			s["val"] = 1
			return s, nil
		}).
		AddSubgraph("sub", innerCompiled)
	outer.SetEntryPoint("setup")
	outer.AddEdge("setup", "sub")
	outer.SetFinishPoint("sub")

	compiled, err := outer.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.Nodes["sub"]; !ok {
		t.Error("subgraph node not found in compiled graph")
	}
}

func TestSubgraphNode_ConditionalEdges(t *testing.T) {
	inner := New("branching").
		AddNode("check", func(_ context.Context, s State) (State, error) {
			return s, nil
		}).
		AddNode("path_a", func(_ context.Context, s State) (State, error) {
			s["path"] = "a"
			return s, nil
		}).
		AddNode("path_b", func(_ context.Context, s State) (State, error) {
			s["path"] = "b"
			return s, nil
		})
	inner.SetEntryPoint("check")
	inner.AddConditionalEdge("check", func(s State) string {
		if s["mode"] == "alpha" {
			return "path_a"
		}
		return "path_b"
	})
	inner.SetFinishPoint("path_a")
	inner.SetFinishPoint("path_b")

	compiled, err := inner.Compile()
	if err != nil {
		t.Fatal(err)
	}

	fn := SubgraphNode(compiled)

	result, err := fn(context.Background(), State{"mode": "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if result["path"] != "a" {
		t.Errorf("got path=%v, want a", result["path"])
	}

	result, err = fn(context.Background(), State{"mode": "beta"})
	if err != nil {
		t.Fatal(err)
	}
	if result["path"] != "b" {
		t.Errorf("got path=%v, want b", result["path"])
	}
}
