package graph

import (
	"context"
	"fmt"
)

// SubgraphNode wraps a CompiledGraph as a NodeFunc so it can be used
// as a node within another graph. The subgraph executes inline,
// sharing the parent's state.
func SubgraphNode(sub *CompiledGraph) NodeFunc {
	return func(ctx context.Context, state State) (State, error) {
		current := sub.Entry
		for current != EndNode && current != "" {
			node, ok := sub.Nodes[current]
			if !ok {
				return state, fmt.Errorf("subgraph %q: node %q not found", sub.ID, current)
			}

			var err error
			state, err = node.Fn(ctx, state)
			if err != nil {
				return state, fmt.Errorf("subgraph %q node %q: %w", sub.ID, current, err)
			}

			next := findSubgraphNext(sub, current, state)
			current = next
		}
		return state, nil
	}
}

func findSubgraphNext(g *CompiledGraph, from string, state State) string {
	edges := g.AdjList[from]
	if len(edges) == 0 {
		return ""
	}
	e := edges[0]
	if e.Condition != nil {
		return e.Condition(state)
	}
	return e.To
}

// AddSubgraph registers a compiled graph as a node within this graph.
func (g *StateGraph) AddSubgraph(id string, sub *CompiledGraph) *StateGraph {
	g.nodes[id] = &Node{ID: id, Fn: SubgraphNode(sub)}
	return g
}
