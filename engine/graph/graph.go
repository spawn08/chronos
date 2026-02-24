package graph

import "fmt"

const (
	StartNode = "__start__"
	EndNode   = "__end__"
)

// StateGraph is a builder for defining directed graphs with durable execution.
type StateGraph struct {
	id    string
	nodes map[string]*Node
	edges []*Edge
}

// New creates a new StateGraph with the given ID.
func New(id string) *StateGraph {
	return &StateGraph{
		id:    id,
		nodes: make(map[string]*Node),
	}
}

// AddNode registers a node with the given ID and handler function.
func (g *StateGraph) AddNode(id string, fn NodeFunc) *StateGraph {
	g.nodes[id] = &Node{ID: id, Fn: fn}
	return g
}

// AddInterruptNode registers a node that will pause execution for human approval.
func (g *StateGraph) AddInterruptNode(id string, fn NodeFunc) *StateGraph {
	g.nodes[id] = &Node{ID: id, Fn: fn, Interrupt: true}
	return g
}

// AddEdge adds a static edge from one node to another.
func (g *StateGraph) AddEdge(from, to string) *StateGraph {
	g.edges = append(g.edges, &Edge{From: from, To: to})
	return g
}

// AddConditionalEdge adds a dynamic edge that routes based on state.
func (g *StateGraph) AddConditionalEdge(from string, condition EdgeCondition) *StateGraph {
	g.edges = append(g.edges, &Edge{From: from, Condition: condition})
	return g
}

// SetEntryPoint sets the starting node of the graph.
func (g *StateGraph) SetEntryPoint(nodeID string) *StateGraph {
	return g.AddEdge(StartNode, nodeID)
}

// SetFinishPoint sets the ending node of the graph.
func (g *StateGraph) SetFinishPoint(nodeID string) *StateGraph {
	return g.AddEdge(nodeID, EndNode)
}

// CompiledGraph is the immutable, validated graph ready for execution.
type CompiledGraph struct {
	ID      string
	Nodes   map[string]*Node
	AdjList map[string][]*Edge // from -> edges
	Entry   string
}

// Compile validates the graph and returns a CompiledGraph.
func (g *StateGraph) Compile() (*CompiledGraph, error) {
	// Find entry point
	var entry string
	adj := make(map[string][]*Edge)
	for _, e := range g.edges {
		adj[e.From] = append(adj[e.From], e)
		if e.From == StartNode {
			entry = e.To
		}
	}
	if entry == "" {
		return nil, fmt.Errorf("graph %q: no entry point set", g.id)
	}
	if _, ok := g.nodes[entry]; !ok {
		return nil, fmt.Errorf("graph %q: entry node %q not found", g.id, entry)
	}
	// Validate all edge targets exist
	for _, e := range g.edges {
		if e.To != "" && e.To != EndNode {
			if _, ok := g.nodes[e.To]; !ok {
				return nil, fmt.Errorf("graph %q: edge target %q not found", g.id, e.To)
			}
		}
	}
	return &CompiledGraph{
		ID:      g.id,
		Nodes:   g.nodes,
		AdjList: adj,
		Entry:   entry,
	}, nil
}
