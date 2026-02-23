Add a new node to a Chronos StateGraph.

The node name/purpose is: $ARGUMENTS

## Instructions

1. A node is a `graph.NodeFunc` with signature:
```go
func(ctx context.Context, state graph.State) (graph.State, error)
```

2. `graph.State` is `map[string]any` — read inputs from it, write outputs back to it

3. Add the node to a graph using the builder API in `engine/graph/graph.go`:
```go
g := graph.New("my_graph").
    AddNode("node_name", func(ctx context.Context, s graph.State) (graph.State, error) {
        // Read from state
        input := s["some_key"]
        // Do work
        s["result"] = output
        return s, nil
    })
```

4. For human-in-the-loop nodes that pause for approval:
```go
g.AddInterruptNode("review", handler)
```

5. Connect nodes with edges:
```go
g.AddEdge("node_a", "node_b")                    // static
g.AddConditionalEdge("node_a", routerFunc)         // dynamic
g.SetEntryPoint("first_node")
g.SetFinishPoint("last_node")
```

6. The conditional edge router returns a target node ID:
```go
func(state graph.State) string {
    if state["should_review"] == true {
        return "review"
    }
    return "respond"
}
```

7. Run `go build ./...` to verify
