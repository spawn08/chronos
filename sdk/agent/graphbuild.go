package agent

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/spawn08/chronos/engine/graph"
)

// graphEndSentinel lets a YAML graph route directly to the end of the run
// (graph.EndNode) from a conditional edge's routes/default, without needing a
// trailing no-op node just to satisfy SetFinishPoint.
const graphEndSentinel = "__end__"

// GraphConfig declares a durable multi-node graph in YAML. It compiles to a
// *graph.StateGraph the same shape a Go caller would build with
// graph.New(id).AddNode(...).AddEdge(...); see attachGraph.
type GraphConfig struct {
	Entry  string       `yaml:"entry"`
	Finish string       `yaml:"finish,omitempty"`
	Nodes  []NodeConfig `yaml:"nodes"`
	Edges  []EdgeConfig `yaml:"edges,omitempty"`
}

// NodeConfig declares one graph node. YAML cannot carry arbitrary Go code, so
// Type selects one of four declarative node kinds instead of a hand-written
// graph.NodeFunc:
//
//   - "model" calls the agent's configured provider with Prompt (a Go
//     text/template rendered against {{.state.<key>}}) and writes the reply to
//     OutputKey (default "response").
//   - "tool" calls a tool already registered on the agent (see
//     AgentConfig.Tools) by name, passing State[InputKey] (or the whole state
//     when InputKey is empty) as arguments and writing the result to
//     OutputKey (default "<id>_result").
//   - "subagent" delegates to another AgentConfig in the same file by ID,
//     calling its Chat with State[InputKey] (or State["message"] when
//     InputKey is empty) and writing the reply to OutputKey (default
//     "<id>_response").
//   - "passthrough" merges Set into State — the minimal building block for an
//     Interrupt (HITL) gate that just needs to record a decision on resume.
type NodeConfig struct {
	ID        string         `yaml:"id"`
	Type      string         `yaml:"type"`
	Interrupt bool           `yaml:"interrupt,omitempty"`
	Prompt    string         `yaml:"prompt,omitempty"`     // model
	Tool      string         `yaml:"tool,omitempty"`       // tool
	Agent     string         `yaml:"agent,omitempty"`      // subagent
	InputKey  string         `yaml:"input_key,omitempty"`  // tool, subagent
	OutputKey string         `yaml:"output_key,omitempty"` // model, tool, subagent
	Set       map[string]any `yaml:"set,omitempty"`        // passthrough
}

// EdgeConfig declares one graph edge. A static edge sets To; a conditional
// edge sets Conditional and routes on State[RouteKey] via Routes, falling
// back to Default when the looked-up value has no matching route.
type EdgeConfig struct {
	From        string            `yaml:"from"`
	To          string            `yaml:"to,omitempty"`
	Conditional bool              `yaml:"conditional,omitempty"`
	RouteKey    string            `yaml:"route_key,omitempty"`
	Routes      map[string]string `yaml:"routes,omitempty"`
	Default     string            `yaml:"default,omitempty"`
}

// validateGraphConfig checks structural correctness (node/edge references,
// per-type required fields) that graph.StateGraph.Compile cannot catch itself
// — it only validates static edge targets, not YAML-specific concerns like
// duplicate node ids or conditional-route targets. A nil gc is valid (no
// graph declared).
func validateGraphConfig(agentID string, gc *GraphConfig) error {
	if gc == nil {
		return nil
	}
	if strings.TrimSpace(gc.Entry) == "" {
		return fmt.Errorf("agent %q graph.entry is required", agentID)
	}
	if len(gc.Nodes) == 0 {
		return fmt.Errorf("agent %q graph.nodes must declare at least one node", agentID)
	}
	ids := make(map[string]struct{}, len(gc.Nodes))
	for i := range gc.Nodes {
		n := &gc.Nodes[i]
		if strings.TrimSpace(n.ID) == "" {
			return fmt.Errorf("agent %q graph.nodes[%d].id is required", agentID, i)
		}
		if _, dup := ids[n.ID]; dup {
			return fmt.Errorf("agent %q graph: duplicate node id %q", agentID, n.ID)
		}
		ids[n.ID] = struct{}{}
		switch n.Type {
		case "model":
			if strings.TrimSpace(n.Prompt) == "" {
				return fmt.Errorf("agent %q graph node %q: type model requires prompt", agentID, n.ID)
			}
		case "tool":
			if strings.TrimSpace(n.Tool) == "" {
				return fmt.Errorf("agent %q graph node %q: type tool requires tool", agentID, n.ID)
			}
		case "subagent":
			if strings.TrimSpace(n.Agent) == "" {
				return fmt.Errorf("agent %q graph node %q: type subagent requires agent", agentID, n.ID)
			}
		case "passthrough":
			// no required fields
		default:
			return fmt.Errorf("agent %q graph node %q: unknown type %q (want model, tool, subagent, or passthrough)", agentID, n.ID, n.Type)
		}
	}
	if _, ok := ids[gc.Entry]; !ok {
		return fmt.Errorf("agent %q graph.entry %q is not a declared node", agentID, gc.Entry)
	}
	if gc.Finish != "" {
		if _, ok := ids[gc.Finish]; !ok {
			return fmt.Errorf("agent %q graph.finish %q is not a declared node", agentID, gc.Finish)
		}
	}
	validTarget := func(id string) bool {
		if id == "" || id == graphEndSentinel {
			return true
		}
		_, ok := ids[id]
		return ok
	}
	for i, e := range gc.Edges {
		if strings.TrimSpace(e.From) == "" {
			return fmt.Errorf("agent %q graph.edges[%d].from is required", agentID, i)
		}
		if _, ok := ids[e.From]; !ok {
			return fmt.Errorf("agent %q graph.edges[%d]: from %q is not a declared node", agentID, i, e.From)
		}
		if e.Conditional {
			if strings.TrimSpace(e.RouteKey) == "" {
				return fmt.Errorf("agent %q graph.edges[%d]: conditional edge requires route_key", agentID, i)
			}
			if len(e.Routes) == 0 {
				return fmt.Errorf("agent %q graph.edges[%d]: conditional edge requires at least one entry in routes", agentID, i)
			}
			for val, target := range e.Routes {
				if !validTarget(target) {
					return fmt.Errorf("agent %q graph.edges[%d]: route %q -> %q is not a declared node", agentID, i, val, target)
				}
			}
			if !validTarget(e.Default) {
				return fmt.Errorf("agent %q graph.edges[%d]: default %q is not a declared node", agentID, i, e.Default)
			}
		} else {
			if strings.TrimSpace(e.To) == "" {
				return fmt.Errorf("agent %q graph.edges[%d]: to is required for a non-conditional edge", agentID, i)
			}
			if !validTarget(e.To) {
				return fmt.Errorf("agent %q graph.edges[%d]: to %q is not a declared node", agentID, i, e.To)
			}
		}
	}
	return nil
}

// DurableGraphs returns the compiled graph.CompiledGraph for every agent in
// fc marked `durable: true`, keyed by agent id, from an already-built agents
// map (e.g. the result of BuildAll). An agent absent from agents, or whose
// Graph did not get compiled, is skipped rather than causing a panic — both
// are defensive only (BuildAll either builds every agent in fc and compiles
// every declared graph, or returns an error), so callers driving
// `chronos serve`-style registration (cli/cmd/serve.go, examples/
// yaml_dashboard) can share this instead of each hand-rolling the loop with
// their own (potentially missing) nil checks.
func DurableGraphs(fc *FileConfig, agents map[string]*Agent) map[string]*graph.CompiledGraph {
	out := make(map[string]*graph.CompiledGraph)
	for i := range fc.Agents {
		cfg := &fc.Agents[i]
		if !cfg.Durable {
			continue
		}
		a, ok := agents[cfg.ID]
		if !ok || a.Graph == nil {
			continue
		}
		out[cfg.ID] = a.Graph
	}
	return out
}

// attachGraph compiles cfg.Graph against a's already-built model/tool
// registry (and, for subagent nodes, peers — sibling agents built earlier in
// the same file) and assigns the result directly to a.Graph. It is called
// either from BuildAgent (peers nil, unless WithPeerAgents was passed) or
// from BuildAll's post-build pass (peers populated with the full build set),
// mirroring how AgentConfig.SubAgents is wired in a second pass for the same
// reason: a graph node may reference an agent not yet built at that point in
// the file.
func attachGraph(a *Agent, cfg *AgentConfig, peers map[string]*Agent) error {
	if cfg.Graph == nil {
		return nil
	}
	sg := graph.New(cfg.ID)
	for i := range cfg.Graph.Nodes {
		n := &cfg.Graph.Nodes[i]
		fn, err := buildNodeFunc(n, a, peers)
		if err != nil {
			return fmt.Errorf("agent %q graph node %q: %w", cfg.ID, n.ID, err)
		}
		fn = withNonNilState(fn)
		if n.Interrupt {
			sg.AddInterruptNode(n.ID, fn)
		} else {
			sg.AddNode(n.ID, fn)
		}
	}
	sg.SetEntryPoint(cfg.Graph.Entry)
	if cfg.Graph.Finish != "" {
		sg.SetFinishPoint(cfg.Graph.Finish)
	}
	for _, e := range cfg.Graph.Edges {
		if e.Conditional {
			routeKey := e.RouteKey
			routes := e.Routes
			def := resolveSentinel(e.Default)
			sg.AddConditionalEdge(e.From, func(s graph.State) string {
				if target, ok := routes[fmt.Sprint(s[routeKey])]; ok {
					return resolveSentinel(target)
				}
				return def
			})
		} else {
			sg.AddEdge(e.From, resolveSentinel(e.To))
		}
	}
	compiled, err := sg.Compile()
	if err != nil {
		return fmt.Errorf("agent %q graph: %w", cfg.ID, err)
	}
	a.Graph = compiled
	return nil
}

func resolveSentinel(id string) string {
	if id == graphEndSentinel {
		return graph.EndNode
	}
	return id
}

// withNonNilState guards a NodeFunc against a nil graph.State — a caller
// driving the compiled graph directly (Agent.Run(ctx, nil), rather than
// through os/dashboard's handleStartRun, which already defaults a nil input
// to an empty map) would otherwise panic on the first `s[key] = v` write any
// node type here performs.
func withNonNilState(fn graph.NodeFunc) graph.NodeFunc {
	return func(ctx context.Context, s graph.State) (graph.State, error) {
		if s == nil {
			s = graph.State{}
		}
		return fn(ctx, s)
	}
}

// defaultKey returns configured, or fallback when configured is empty — the
// "use this output_key, or a type-specific default" pattern shared by the
// model/tool/subagent node builders below.
func defaultKey(configured, fallback string) string {
	if configured == "" {
		return fallback
	}
	return configured
}

// stringInput resolves state[inputKey] as a string, when inputKey is set.
// Unlike a bare type assertion, a missing key or wrong type is a reported
// error rather than a silent fallback — a config author who sets input_key
// expects a specific upstream node's output there, and finding the wrong
// thing there means something upstream misbehaved, not that this node should
// quietly guess.
func stringInput(s graph.State, inputKey, fallback string) (string, error) {
	if inputKey == "" {
		return fallback, nil
	}
	v, ok := s[inputKey]
	if !ok {
		return "", fmt.Errorf("input_key %q not found in state", inputKey)
	}
	str, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("input_key %q is %T, want a string", inputKey, v)
	}
	return str, nil
}

// mapInput resolves state[inputKey] as a map[string]any, when inputKey is
// set, with the same fail-loud contract as stringInput.
func mapInput(s graph.State, inputKey string) (map[string]any, error) {
	if inputKey == "" {
		return map[string]any(s), nil
	}
	v, ok := s[inputKey]
	if !ok {
		return nil, fmt.Errorf("input_key %q not found in state", inputKey)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("input_key %q is %T, want a map", inputKey, v)
	}
	return m, nil
}

// buildNodeFunc translates one NodeConfig into a graph.NodeFunc closure over
// the agent's already-built runtime (model, tool registry) and, for subagent
// nodes, the peer-agent map. validateGraphConfig has already checked
// per-type required fields, so errors here are runtime-only (unregistered
// tool, unresolved peer, bad template).
func buildNodeFunc(n *NodeConfig, a *Agent, peers map[string]*Agent) (graph.NodeFunc, error) {
	switch n.Type {
	case "model":
		if a.Model == nil {
			return nil, fmt.Errorf("type model requires the agent to have a model configured")
		}
		tmpl, err := template.New(n.ID).Parse(n.Prompt)
		if err != nil {
			return nil, fmt.Errorf("parse prompt template: %w", err)
		}
		outputKey := defaultKey(n.OutputKey, "response")
		return func(ctx context.Context, s graph.State) (graph.State, error) {
			var buf bytes.Buffer
			if err := tmpl.Execute(&buf, map[string]any{"state": map[string]any(s)}); err != nil {
				return s, fmt.Errorf("render prompt template: %w", err)
			}
			// Routes through Agent.Chat rather than a.Model.Chat directly, so a
			// model node gets the same system prompt, input/output guardrails,
			// memory/RAG injection, tool-calling loop, hooks, and tracing as any
			// other model call the agent makes — not a bare, unguarded API call.
			resp, err := a.Chat(ctx, buf.String())
			if err != nil {
				return s, err
			}
			s[outputKey] = resp.Content
			return s, nil
		}, nil

	case "tool":
		if _, ok := a.Tools.Get(n.Tool); !ok {
			return nil, fmt.Errorf("tool %q is not registered on this agent (add it under tools:)", n.Tool)
		}
		toolName := n.Tool
		inputKey := n.InputKey
		outputKey := defaultKey(n.OutputKey, n.ID+"_result")
		return func(ctx context.Context, s graph.State) (graph.State, error) {
			input, err := mapInput(s, inputKey)
			if err != nil {
				return s, err
			}
			// Routes through Registry.Execute (not the Definition's Handler
			// directly) so permission (deny/require_approval), confirmation, and
			// user-input gating are enforced exactly as they are for a tool the
			// model itself decides to call — a graph-declared tool node is not a
			// backdoor around those checks.
			result, err := a.Tools.Execute(ctx, toolName, input)
			if err != nil {
				return s, err
			}
			s[outputKey] = result
			return s, nil
		}, nil

	case "subagent":
		sub, ok := peers[n.Agent]
		if !ok {
			return nil, fmt.Errorf("subagent %q not found among built peer agents (it must be defined in the same config file)", n.Agent)
		}
		inputKey := n.InputKey
		outputKey := defaultKey(n.OutputKey, n.ID+"_response")
		return func(ctx context.Context, s graph.State) (graph.State, error) {
			fallback, _ := s["message"].(string)
			msg, err := stringInput(s, inputKey, fallback)
			if err != nil {
				return s, err
			}
			resp, err := sub.Chat(ctx, msg)
			if err != nil {
				return s, err
			}
			s[outputKey] = resp.Content
			return s, nil
		}, nil

	case "passthrough":
		set := n.Set
		return func(_ context.Context, s graph.State) (graph.State, error) {
			for k, v := range set {
				s[k] = v
			}
			return s, nil
		}, nil

	default:
		return nil, fmt.Errorf("unknown node type %q", n.Type)
	}
}
