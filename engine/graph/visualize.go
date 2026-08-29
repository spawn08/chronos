package graph

import (
	"fmt"
	"sort"
	"strings"
)

// ToMermaid generates a Mermaid flowchart definition of the compiled graph.
func (cg *CompiledGraph) ToMermaid() string {
	var b strings.Builder
	b.WriteString("flowchart TD\n")

	b.WriteString("    __start__([Start])\n")
	b.WriteString("    __end__([End])\n")

	nodeIDs := sortedNodeIDs(cg.Nodes)
	for _, id := range nodeIDs {
		node := cg.Nodes[id]
		if node.Interrupt {
			b.WriteString(fmt.Sprintf("    %s{{%s}}\n", id, id))
		} else {
			b.WriteString(fmt.Sprintf("    %s[%s]\n", id, id))
		}
	}

	for from, edges := range cg.AdjList {
		for _, e := range edges {
			src := from
			dst := e.To
			if dst == "" {
				dst = fmt.Sprintf("%s_cond{?}", from)
				b.WriteString(fmt.Sprintf("    %s --> %s\n", src, dst))
			} else {
				b.WriteString(fmt.Sprintf("    %s --> %s\n", src, dst))
			}
		}
	}

	return b.String()
}

// ToDOT generates a Graphviz DOT format representation of the compiled graph.
func (cg *CompiledGraph) ToDOT() string {
	var b strings.Builder
	b.WriteString("digraph ")
	b.WriteString(sanitizeDOTID(cg.ID))
	b.WriteString(" {\n")
	b.WriteString("    rankdir=TD;\n")
	b.WriteString("    node [shape=box, style=rounded];\n\n")

	b.WriteString("    __start__ [label=\"Start\", shape=circle, style=filled, fillcolor=\"#4CAF50\", fontcolor=white];\n")
	b.WriteString("    __end__ [label=\"End\", shape=doublecircle, style=filled, fillcolor=\"#f44336\", fontcolor=white];\n\n")

	nodeIDs := sortedNodeIDs(cg.Nodes)
	for _, id := range nodeIDs {
		node := cg.Nodes[id]
		if node.Interrupt {
			b.WriteString(fmt.Sprintf("    %s [label=%q, shape=diamond, style=filled, fillcolor=\"#FF9800\", fontcolor=white];\n",
				sanitizeDOTID(id), id))
		} else {
			b.WriteString(fmt.Sprintf("    %s [label=%q];\n", sanitizeDOTID(id), id))
		}
	}

	b.WriteString("\n")

	for from, edges := range cg.AdjList {
		for _, e := range edges {
			src := sanitizeDOTID(from)
			if e.To != "" {
				dst := sanitizeDOTID(e.To)
				b.WriteString(fmt.Sprintf("    %s -> %s;\n", src, dst))
			} else {
				b.WriteString(fmt.Sprintf("    %s -> %s [label=\"conditional\", style=dashed];\n",
					src, sanitizeDOTID(from+"_cond")))
			}
		}
	}

	b.WriteString("}\n")
	return b.String()
}

// GraphView is a JSON-serializable description of a compiled graph's topology
// (nodes + edges), for clients — such as the dashboard UI (os/dashboard) —
// that render their own visualization instead of parsing Mermaid/DOT text.
// Synthetic start/end markers are included so a renderer can draw the same
// entry/exit points ToMermaid/ToDOT show.
type GraphView struct {
	ID    string          `json:"id"`
	Entry string          `json:"entry"`
	Nodes []GraphViewNode `json:"nodes"`
	Edges []GraphViewEdge `json:"edges"`
}

// GraphViewNode describes one node in a GraphView. Kind is "start", "end",
// "interrupt", or "node".
type GraphViewNode struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

// GraphViewEdge describes one transition in a GraphView. To is empty when
// Conditional is true and the target is decided at runtime (see
// EdgeCondition); a renderer should badge such a node rather than draw an
// arrow to an unknown destination.
type GraphViewEdge struct {
	From        string `json:"from"`
	To          string `json:"to,omitempty"`
	Conditional bool   `json:"conditional"`
}

// ToJSON returns a serializable view of the graph's topology.
func (cg *CompiledGraph) ToJSON() GraphView {
	view := GraphView{ID: cg.ID, Entry: cg.Entry}

	view.Nodes = append(view.Nodes, GraphViewNode{ID: StartNode, Kind: "start"})
	for _, id := range sortedNodeIDs(cg.Nodes) {
		kind := "node"
		if cg.Nodes[id].Interrupt {
			kind = "interrupt"
		}
		view.Nodes = append(view.Nodes, GraphViewNode{ID: id, Kind: kind})
	}
	view.Nodes = append(view.Nodes, GraphViewNode{ID: EndNode, Kind: "end"})

	fromIDs := make([]string, 0, len(cg.AdjList))
	for from := range cg.AdjList {
		fromIDs = append(fromIDs, from)
	}
	sort.Strings(fromIDs)
	for _, from := range fromIDs {
		for _, e := range cg.AdjList[from] {
			view.Edges = append(view.Edges, GraphViewEdge{From: from, To: e.To, Conditional: e.Condition != nil})
		}
	}

	return view
}

func sortedNodeIDs(nodes map[string]*Node) []string {
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sanitizeDOTID(s string) string {
	r := strings.NewReplacer("-", "_", " ", "_", ".", "_")
	return r.Replace(s)
}
