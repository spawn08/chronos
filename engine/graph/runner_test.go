package graph

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/stream"
	"github.com/spawn08/chronos/os/trace"
	"github.com/spawn08/chronos/storage"
)

// --- in-memory storage for runner tests ---

type runnerTestStorage struct {
	mu          sync.Mutex
	sessions    map[string]*storage.Session
	events      []*storage.Event
	checkpoints []*storage.Checkpoint
	traces      []*storage.Trace
	auditLogs   []*storage.AuditLog
	memory      map[string]*storage.MemoryRecord
}

func newRunnerTestStorage() *runnerTestStorage {
	return &runnerTestStorage{
		sessions:    make(map[string]*storage.Session),
		checkpoints: make([]*storage.Checkpoint, 0),
		traces:      make([]*storage.Trace, 0),
		memory:      make(map[string]*storage.MemoryRecord),
	}
}

func (s *runnerTestStorage) CreateSession(_ context.Context, sess *storage.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.ID] = sess
	return nil
}

func (s *runnerTestStorage) GetSession(_ context.Context, id string) (*storage.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		return sess, nil
	}
	return nil, errors.New("session not found")
}

func (s *runnerTestStorage) UpdateSession(_ context.Context, sess *storage.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess.ID] = sess
	return nil
}

func (s *runnerTestStorage) ListSessions(_ context.Context, _ string, _, _ int) ([]*storage.Session, error) {
	return nil, nil
}

func (s *runnerTestStorage) AppendEvent(_ context.Context, e *storage.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	return nil
}

func (s *runnerTestStorage) ListEvents(_ context.Context, _ string, _ int64) ([]*storage.Event, error) {
	return nil, nil
}

func (s *runnerTestStorage) SaveCheckpoint(_ context.Context, cp *storage.Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpoints = append(s.checkpoints, cp)
	return nil
}

func (s *runnerTestStorage) GetCheckpoint(_ context.Context, id string) (*storage.Checkpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cp := range s.checkpoints {
		if cp.ID == id {
			return cp, nil
		}
	}
	return nil, errors.New("checkpoint not found")
}

func (s *runnerTestStorage) GetLatestCheckpoint(_ context.Context, sessionID string) (*storage.Checkpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latest *storage.Checkpoint
	for _, cp := range s.checkpoints {
		if cp.SessionID == sessionID {
			if latest == nil || cp.SeqNum > latest.SeqNum {
				latest = cp
			}
		}
	}
	if latest == nil {
		return nil, errors.New("no checkpoint")
	}
	return latest, nil
}

func (s *runnerTestStorage) ListCheckpoints(_ context.Context, _ string) ([]*storage.Checkpoint, error) {
	return nil, nil
}

func (s *runnerTestStorage) InsertTrace(_ context.Context, t *storage.Trace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.traces = append(s.traces, t)
	return nil
}

func (s *runnerTestStorage) GetTrace(_ context.Context, _ string) (*storage.Trace, error) {
	return nil, nil
}

func (s *runnerTestStorage) ListTraces(_ context.Context, _ string) ([]*storage.Trace, error) {
	return nil, nil
}

func (s *runnerTestStorage) AppendAuditLog(_ context.Context, l *storage.AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.auditLogs = append(s.auditLogs, l)
	return nil
}

func (s *runnerTestStorage) ListAuditLogs(_ context.Context, _ string, _, _ int) ([]*storage.AuditLog, error) {
	return nil, nil
}

func (s *runnerTestStorage) PutMemory(_ context.Context, m *storage.MemoryRecord) error {
	s.memory[m.ID] = m
	return nil
}

func (s *runnerTestStorage) GetMemory(_ context.Context, _, _ string) (*storage.MemoryRecord, error) {
	return nil, errors.New("not found")
}

func (s *runnerTestStorage) ListMemory(_ context.Context, _, _ string) ([]*storage.MemoryRecord, error) {
	return nil, nil
}

func (s *runnerTestStorage) DeleteMemory(_ context.Context, _ string) error { return nil }
func (s *runnerTestStorage) Migrate(_ context.Context) error                { return nil }
func (s *runnerTestStorage) Close() error                                   { return nil }

// --- helpers ---

func buildLinearGraph(nodes ...string) *CompiledGraph {
	g := New("test-graph")
	for _, id := range nodes {
		nodeName := id
		g.AddNode(nodeName, func(_ context.Context, state State) (State, error) {
			visited, _ := state["visited"].(string)
			state["visited"] = visited + "," + nodeName
			return state, nil
		})
	}
	g.SetEntryPoint(nodes[0])
	for i := 0; i < len(nodes)-1; i++ {
		g.AddEdge(nodes[i], nodes[i+1])
	}
	g.SetFinishPoint(nodes[len(nodes)-1])

	compiled, _ := g.Compile()
	return compiled
}

// drainChannel reads all events from the stream channel with a timeout.
func drainChannel(ch <-chan StreamEvent, timeout time.Duration) []StreamEvent {
	var events []StreamEvent
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, evt)
		case <-timer.C:
			return events
		}
	}
}

// --- P0-006 tests: Runner → SSE Broker ---

func TestRunner_EmitsToBroker(t *testing.T) {
	store := newRunnerTestStorage()
	compiled := buildLinearGraph("step_a", "step_b")

	broker := stream.NewBroker()
	sub := broker.Subscribe("test-sub")
	defer broker.Unsubscribe("test-sub")

	runner := NewRunner(compiled, store).WithBroker(broker)

	var brokerEvents []stream.Event
	done := make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		for {
			select {
			case evt, ok := <-sub:
				if !ok {
					return
				}
				brokerEvents = append(brokerEvents, evt)
			case <-timer.C:
				return
			}
		}
	}()

	result, err := runner.Run(context.Background(), "session-1", State{"visited": ""})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != RunStatusCompleted {
		t.Errorf("status = %q, want %q", result.Status, RunStatusCompleted)
	}

	<-done

	if len(brokerEvents) == 0 {
		t.Fatal("expected broker to receive events, got 0")
	}

	typeSet := make(map[string]bool)
	for _, e := range brokerEvents {
		typeSet[e.Type] = true
	}
	for _, expected := range []string{"node_start", "node_end", "completed"} {
		if !typeSet[expected] {
			t.Errorf("missing broker event type %q; got types: %v", expected, typeSet)
		}
	}
}

func TestRunner_NoBrokerStillWorks(t *testing.T) {
	store := newRunnerTestStorage()
	compiled := buildLinearGraph("only_node")

	runner := NewRunner(compiled, store) // no broker attached

	result, err := runner.Run(context.Background(), "session-no-broker", State{"visited": ""})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != RunStatusCompleted {
		t.Errorf("status = %q, want %q", result.Status, RunStatusCompleted)
	}
}

func TestRunner_LocalStreamChannel(t *testing.T) {
	store := newRunnerTestStorage()
	compiled := buildLinearGraph("node1", "node2", "node3")

	runner := NewRunner(compiled, store)
	ch := runner.Stream()

	result, err := runner.Run(context.Background(), "session-stream", State{"visited": ""})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != RunStatusCompleted {
		t.Errorf("status = %q, want %q", result.Status, RunStatusCompleted)
	}

	events := drainChannel(ch, 500*time.Millisecond)
	if len(events) == 0 {
		t.Fatal("expected local stream channel to receive events")
	}

	var nodeStarts, nodeEnds int
	for _, e := range events {
		switch e.Type {
		case "node_start":
			nodeStarts++
		case "node_end":
			nodeEnds++
		}
	}
	if nodeStarts != 3 {
		t.Errorf("nodeStarts = %d, want 3", nodeStarts)
	}
	if nodeEnds != 3 {
		t.Errorf("nodeEnds = %d, want 3", nodeEnds)
	}
}

func TestRunner_EmitsEdgeTransition(t *testing.T) {
	store := newRunnerTestStorage()
	compiled := buildLinearGraph("a", "b")

	runner := NewRunner(compiled, store)
	ch := runner.Stream()

	_, err := runner.Run(context.Background(), "s1", State{"visited": ""})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	events := drainChannel(ch, 500*time.Millisecond)
	foundEdgeTransition := false
	for _, e := range events {
		if e.Type == "edge_transition" {
			foundEdgeTransition = true
			if e.NodeID != "b" {
				t.Errorf("edge_transition NodeID = %q, want %q", e.NodeID, "b")
			}
		}
	}
	if !foundEdgeTransition {
		t.Error("expected edge_transition event")
	}
}

func TestRunner_EmitsErrorOnNodeFailure(t *testing.T) {
	store := newRunnerTestStorage()

	g := New("fail-graph")
	g.AddNode("fail_node", func(_ context.Context, _ State) (State, error) {
		return nil, errors.New("node exploded")
	})
	g.SetEntryPoint("fail_node")
	g.SetFinishPoint("fail_node")
	compiled, _ := g.Compile()

	broker := stream.NewBroker()
	sub := broker.Subscribe("err-sub")
	defer broker.Unsubscribe("err-sub")

	runner := NewRunner(compiled, store).WithBroker(broker)
	ch := runner.Stream()

	_, err := runner.Run(context.Background(), "s-fail", State{})
	if err == nil {
		t.Fatal("expected error from failing node")
	}

	// Check local stream
	localEvents := drainChannel(ch, 500*time.Millisecond)
	foundError := false
	for _, e := range localEvents {
		if e.Type == "error" && e.Error == "node exploded" {
			foundError = true
		}
	}
	if !foundError {
		t.Error("expected error event on local stream")
	}

	// Check broker
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	foundBrokerErr := false
	for {
		select {
		case evt := <-sub:
			if evt.Type == "error" {
				foundBrokerErr = true
			}
		case <-timer.C:
			goto done
		}
	}
done:
	if !foundBrokerErr {
		t.Error("expected error event on broker")
	}
}

func TestRunner_EmitsInterruptEvent(t *testing.T) {
	store := newRunnerTestStorage()

	g := New("interrupt-graph")
	g.AddNode("normal", func(_ context.Context, state State) (State, error) {
		state["step"] = "done"
		return state, nil
	})
	g.AddInterruptNode("approval", func(_ context.Context, state State) (State, error) {
		state["approved"] = true
		return state, nil
	})
	g.SetEntryPoint("normal")
	g.AddEdge("normal", "approval")
	g.SetFinishPoint("approval")
	compiled, _ := g.Compile()

	runner := NewRunner(compiled, store)
	ch := runner.Stream()

	result, err := runner.Run(context.Background(), "s-interrupt", State{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != RunStatusPaused {
		t.Errorf("status = %q, want %q", result.Status, RunStatusPaused)
	}

	events := drainChannel(ch, 500*time.Millisecond)
	foundInterrupt := false
	for _, e := range events {
		if e.Type == "interrupt" {
			foundInterrupt = true
		}
	}
	if !foundInterrupt {
		t.Error("expected interrupt event in stream")
	}
}

func TestRunner_EmitTimestamp(t *testing.T) {
	store := newRunnerTestStorage()
	compiled := buildLinearGraph("single")

	runner := NewRunner(compiled, store)
	ch := runner.Stream()

	before := time.Now()
	_, err := runner.Run(context.Background(), "s-ts", State{"visited": ""})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	events := drainChannel(ch, 500*time.Millisecond)
	for _, e := range events {
		if e.Timestamp.Before(before) {
			t.Errorf("event timestamp %v is before run start %v", e.Timestamp, before)
		}
	}
}

// --- P0-007 tests: Runner → trace.Collector ---

func TestRunner_TracesGraphExecution(t *testing.T) {
	store := newRunnerTestStorage()
	compiled := buildLinearGraph("step1", "step2")

	collector := trace.NewCollector(store)
	runner := NewRunner(compiled, store).WithTracer(collector)

	result, err := runner.Run(context.Background(), "s-trace", State{"visited": ""})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != RunStatusCompleted {
		t.Errorf("status = %q, want %q", result.Status, RunStatusCompleted)
	}

	store.mu.Lock()
	traces := make([]*storage.Trace, len(store.traces))
	copy(traces, store.traces)
	store.mu.Unlock()

	if len(traces) == 0 {
		t.Fatal("expected trace spans to be recorded")
	}

	kindCounts := make(map[string]int)
	for _, tr := range traces {
		kindCounts[tr.Kind]++
	}

	if kindCounts["graph"] < 1 {
		t.Errorf("expected at least 1 graph-level span, got %d", kindCounts["graph"])
	}
	if kindCounts["node"] < 2 {
		t.Errorf("expected at least 2 node-level spans (step1, step2), got %d", kindCounts["node"])
	}
}

func TestRunner_TracesNodeFailure(t *testing.T) {
	store := newRunnerTestStorage()

	g := New("fail-graph")
	g.AddNode("boom", func(_ context.Context, _ State) (State, error) {
		return nil, errors.New("kaboom")
	})
	g.SetEntryPoint("boom")
	g.SetFinishPoint("boom")
	compiled, _ := g.Compile()

	collector := trace.NewCollector(store)
	runner := NewRunner(compiled, store).WithTracer(collector)

	_, err := runner.Run(context.Background(), "s-fail-trace", State{})
	if err == nil {
		t.Fatal("expected error")
	}

	store.mu.Lock()
	traces := make([]*storage.Trace, len(store.traces))
	copy(traces, store.traces)
	store.mu.Unlock()

	foundErrorSpan := false
	for _, tr := range traces {
		if tr.Error != "" {
			foundErrorSpan = true
		}
	}
	if !foundErrorSpan {
		t.Error("expected at least one span with error")
	}
}

func TestRunner_NoTracerStillWorks(t *testing.T) {
	store := newRunnerTestStorage()
	compiled := buildLinearGraph("x")

	runner := NewRunner(compiled, store) // no tracer
	result, err := runner.Run(context.Background(), "s-no-trace", State{"visited": ""})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != RunStatusCompleted {
		t.Errorf("status = %q, want %q", result.Status, RunStatusCompleted)
	}

	store.mu.Lock()
	traceCount := len(store.traces)
	store.mu.Unlock()

	if traceCount != 0 {
		t.Errorf("expected 0 traces without tracer, got %d", traceCount)
	}
}

func TestRunner_TracesInterruptSpan(t *testing.T) {
	store := newRunnerTestStorage()

	g := New("intr-graph")
	g.AddNode("pre", func(_ context.Context, state State) (State, error) {
		state["pre"] = true
		return state, nil
	})
	g.AddInterruptNode("wait", func(_ context.Context, state State) (State, error) {
		return state, nil
	})
	g.SetEntryPoint("pre")
	g.AddEdge("pre", "wait")
	g.SetFinishPoint("wait")
	compiled, _ := g.Compile()

	collector := trace.NewCollector(store)
	runner := NewRunner(compiled, store).WithTracer(collector)

	result, err := runner.Run(context.Background(), "s-intr-trace", State{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != RunStatusPaused {
		t.Errorf("status = %q, want %q", result.Status, RunStatusPaused)
	}

	store.mu.Lock()
	traces := make([]*storage.Trace, len(store.traces))
	copy(traces, store.traces)
	store.mu.Unlock()

	foundGraphSpan := false
	for _, tr := range traces {
		if tr.Kind == "graph" && tr.Error == "paused at interrupt node wait" {
			foundGraphSpan = true
		}
	}
	if !foundGraphSpan {
		t.Error("expected graph span to note pause at interrupt node")
	}
}

func TestRunner_BrokerAndTracerCombined(t *testing.T) {
	store := newRunnerTestStorage()
	compiled := buildLinearGraph("alpha", "beta")

	broker := stream.NewBroker()
	sub := broker.Subscribe("combo-sub")
	defer broker.Unsubscribe("combo-sub")

	collector := trace.NewCollector(store)
	runner := NewRunner(compiled, store).WithBroker(broker).WithTracer(collector)

	result, err := runner.Run(context.Background(), "s-combo", State{"visited": ""})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != RunStatusCompleted {
		t.Errorf("status = %q, want %q", result.Status, RunStatusCompleted)
	}

	// Verify broker received events
	var brokerEvents []stream.Event
	timer := time.NewTimer(500 * time.Millisecond)
	for {
		select {
		case evt := <-sub:
			brokerEvents = append(brokerEvents, evt)
		case <-timer.C:
			goto checkBroker
		}
	}
checkBroker:
	timer.Stop()
	if len(brokerEvents) == 0 {
		t.Error("broker should have received events")
	}

	// Verify traces recorded
	store.mu.Lock()
	traceCount := len(store.traces)
	store.mu.Unlock()
	if traceCount == 0 {
		t.Error("traces should have been recorded")
	}
}

func TestRunner_WithBrokerReturnsSelf(t *testing.T) {
	store := newRunnerTestStorage()
	compiled := buildLinearGraph("x")
	runner := NewRunner(compiled, store)

	broker := stream.NewBroker()
	got := runner.WithBroker(broker)
	if got != runner {
		t.Error("WithBroker should return the same Runner for chaining")
	}
}

func TestRunner_WithTracerReturnsSelf(t *testing.T) {
	store := newRunnerTestStorage()
	compiled := buildLinearGraph("x")
	runner := NewRunner(compiled, store)

	collector := trace.NewCollector(store)
	got := runner.WithTracer(collector)
	if got != runner {
		t.Error("WithTracer should return the same Runner for chaining")
	}
}

func TestRunner_ResumeWithTracer(t *testing.T) {
	store := newRunnerTestStorage()

	g := New("resume-graph")
	g.AddNode("start_node", func(_ context.Context, state State) (State, error) {
		state["started"] = true
		return state, nil
	})
	g.AddInterruptNode("pause_node", func(_ context.Context, state State) (State, error) {
		state["paused"] = true
		return state, nil
	})
	g.SetEntryPoint("start_node")
	g.AddEdge("start_node", "pause_node")
	g.SetFinishPoint("pause_node")
	compiled, _ := g.Compile()

	collector := trace.NewCollector(store)
	runner := NewRunner(compiled, store).WithTracer(collector)

	// Run until pause
	result, err := runner.Run(context.Background(), "s-resume", State{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != RunStatusPaused {
		t.Fatalf("status = %q, want paused", result.Status)
	}

	store.mu.Lock()
	tracesBeforeResume := len(store.traces)
	store.mu.Unlock()

	// Resume — since pause_node is an interrupt node, it will pause again,
	// but the important thing is that the runner was configured with a tracer
	// and additional trace spans are recorded.
	runner2 := NewRunner(compiled, store).WithTracer(collector)
	result2, err := runner2.Resume(context.Background(), "s-resume")
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	// Interrupt node pauses again on resume
	if result2.Status != RunStatusPaused {
		t.Errorf("resumed status = %q, want paused", result2.Status)
	}

	store.mu.Lock()
	tracesAfterResume := len(store.traces)
	store.mu.Unlock()

	if tracesAfterResume <= tracesBeforeResume {
		t.Errorf("resume should create additional traces; before=%d, after=%d", tracesBeforeResume, tracesAfterResume)
	}
}

func TestRunner_ConditionalEdgeWithBroker(t *testing.T) {
	store := newRunnerTestStorage()

	g := New("cond-graph")
	g.AddNode("router", func(_ context.Context, state State) (State, error) {
		state["routed"] = true
		return state, nil
	})
	g.AddNode("path_a", func(_ context.Context, state State) (State, error) {
		state["path"] = "a"
		return state, nil
	})
	g.AddNode("path_b", func(_ context.Context, state State) (State, error) {
		state["path"] = "b"
		return state, nil
	})
	g.SetEntryPoint("router")
	g.AddConditionalEdge("router", func(state State) string {
		if v, _ := state["choose"].(string); v == "b" {
			return "path_b"
		}
		return "path_a"
	})
	g.SetFinishPoint("path_a")
	g.SetFinishPoint("path_b")
	compiled, _ := g.Compile()

	broker := stream.NewBroker()
	sub := broker.Subscribe("cond-sub")
	defer broker.Unsubscribe("cond-sub")

	runner := NewRunner(compiled, store).WithBroker(broker)
	result, err := runner.Run(context.Background(), "s-cond", State{"choose": "b"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != RunStatusCompleted {
		t.Errorf("status = %q, want completed", result.Status)
	}
	if path, _ := result.State["path"].(string); path != "b" {
		t.Errorf("path = %q, want %q", path, "b")
	}

	var brokerEvents []stream.Event
	timer := time.NewTimer(500 * time.Millisecond)
	for {
		select {
		case evt := <-sub:
			brokerEvents = append(brokerEvents, evt)
		case <-timer.C:
			goto done
		}
	}
done:
	timer.Stop()

	if len(brokerEvents) == 0 {
		t.Error("expected broker events from conditional graph execution")
	}
}

func TestRunner_ManyNodes(t *testing.T) {
	store := newRunnerTestStorage()
	nodeNames := make([]string, 20)
	for i := range nodeNames {
		nodeNames[i] = fmt.Sprintf("node_%02d", i)
	}
	compiled := buildLinearGraph(nodeNames...)

	broker := stream.NewBroker()
	broker.Subscribe("many-sub")
	defer broker.Unsubscribe("many-sub")

	collector := trace.NewCollector(store)
	runner := NewRunner(compiled, store).WithBroker(broker).WithTracer(collector)

	result, err := runner.Run(context.Background(), "s-many", State{"visited": ""})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != RunStatusCompleted {
		t.Errorf("status = %q, want completed", result.Status)
	}

	// Verify checkpoints
	store.mu.Lock()
	cpCount := len(store.checkpoints)
	traceCount := len(store.traces)
	store.mu.Unlock()

	if cpCount < 20 {
		t.Errorf("expected at least 20 checkpoints for 20 nodes, got %d", cpCount)
	}

	// node spans (20) + graph span start/end (2 inserts for 1 span) = at least 22
	if traceCount < 20 {
		t.Errorf("expected at least 20 trace inserts, got %d", traceCount)
	}
}
