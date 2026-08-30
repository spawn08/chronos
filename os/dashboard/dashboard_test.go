package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/hooks"
	"github.com/spawn08/chronos/engine/stream"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/memory"
)

// appendNode returns a node function that appends id to the "path" state key,
// so a test can assert exactly which nodes executed (and in what order) after
// a resume or time-travel.
func appendNode(id string) graph.NodeFunc {
	return func(_ context.Context, s graph.State) (graph.State, error) {
		path, _ := s["path"].(string)
		s["path"] = path + id
		return s, nil
	}
}

// linearGraph compiles a 3-node a->b->c graph (id "wf").
func linearGraph(t *testing.T) *graph.CompiledGraph {
	t.Helper()
	g := graph.New("wf").
		AddNode("a", appendNode("a")).
		AddNode("b", appendNode("b")).
		AddNode("c", appendNode("c")).
		SetEntryPoint("a").
		AddEdge("a", "b").
		AddEdge("b", "c").
		SetFinishPoint("c")
	cg, err := g.Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return cg
}

// hitlGraph compiles a graph with an interrupt gate (id "hitl"): prepare ->
// gate(interrupt) -> finish.
func hitlGraph(t *testing.T) *graph.CompiledGraph {
	t.Helper()
	g := graph.New("hitl").
		AddNode("prepare", appendNode("prepare")).
		AddInterruptNode("gate", appendNode("gate")).
		AddNode("finish", appendNode("finish")).
		SetEntryPoint("prepare").
		AddEdge("prepare", "gate").
		AddEdge("gate", "finish").
		SetFinishPoint("finish")
	cg, err := g.Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return cg
}

func newTestHandler(store storage.Storage, graphs GraphRegistry) *Handler {
	h := New(store, stream.NewBroker())
	if graphs != nil {
		h = h.WithGraphs(graphs)
	}
	return h
}

func doJSON(h *Handler, method, target string, body any) *httptest.ResponseRecorder {
	var r *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, target, bytes.NewReader(b))
	} else {
		r = httptest.NewRequest(method, target, http.NoBody)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHandler_UnknownEndpoint(t *testing.T) {
	h := newTestHandler(memory.New(), nil)
	w := doJSON(h, http.MethodGet, "/nope", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandler_Checkpoints(t *testing.T) {
	store := memory.New()
	ctx := context.Background()
	now := time.Now()
	if err := store.CreateSession(ctx, &storage.Session{ID: "s1", AgentID: "wf-agent", Status: "running", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCheckpoint(ctx, &storage.Checkpoint{ID: "cp1", SessionID: "s1", RunID: "r1", NodeID: "b", State: map[string]any{"path": "a"}, SeqNum: 1, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	h := newTestHandler(store, nil)

	t.Run("missing session_id is a 400", func(t *testing.T) {
		w := doJSON(h, http.MethodGet, "/checkpoints", nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("lists checkpoints for the session", func(t *testing.T) {
		w := doJSON(h, http.MethodGet, "/checkpoints?session_id=s1", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
		}
		var resp struct {
			Checkpoints []*storage.Checkpoint `json:"checkpoints"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Checkpoints) != 1 || resp.Checkpoints[0].ID != "cp1" {
			t.Errorf("checkpoints = %+v, want [cp1]", resp.Checkpoints)
		}
	})

	t.Run("wrong method is 405", func(t *testing.T) {
		w := doJSON(h, http.MethodPost, "/checkpoints?session_id=s1", nil)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want 405", w.Code)
		}
	})
}

func TestHandler_Graph(t *testing.T) {
	store := memory.New()
	ctx := context.Background()
	now := time.Now()
	if err := store.CreateSession(ctx, &storage.Session{ID: "s1", AgentID: "wf-agent", Status: "running", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	t.Run("501 without a registered graph", func(t *testing.T) {
		h := newTestHandler(store, nil)
		w := doJSON(h, http.MethodGet, "/graph?session_id=s1", nil)
		if w.Code != http.StatusNotImplemented {
			t.Errorf("status = %d, want 501", w.Code)
		}
	})

	t.Run("returns the topology for the session's agent", func(t *testing.T) {
		h := newTestHandler(store, GraphRegistry{"wf-agent": linearGraph(t)})
		w := doJSON(h, http.MethodGet, "/graph?session_id=s1", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
		}
		var resp struct {
			GraphID string          `json:"graph_id"`
			View    graph.GraphView `json:"view"`
			Mermaid string          `json:"mermaid"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.GraphID != "wf" {
			t.Errorf("graph_id = %q, want wf", resp.GraphID)
		}
		if len(resp.View.Nodes) != 5 { // start, a, b, c, end
			t.Errorf("got %d nodes, want 5", len(resp.View.Nodes))
		}
		if resp.Mermaid == "" {
			t.Error("mermaid source is empty")
		}
	})

	t.Run("501 for a session whose agent has no registered graph", func(t *testing.T) {
		h := newTestHandler(store, GraphRegistry{"other-agent": linearGraph(t)})
		w := doJSON(h, http.MethodGet, "/graph?session_id=s1", nil)
		if w.Code != http.StatusNotImplemented {
			t.Errorf("status = %d, want 501", w.Code)
		}
	})

	// An unknown session_id must be a 404 (the session doesn't exist), not a
	// 501 (a feature isn't configured) — the two failure modes were once
	// conflated by a single resolveGraph error.
	t.Run("404 for an unknown session", func(t *testing.T) {
		h := newTestHandler(store, GraphRegistry{"wf-agent": linearGraph(t)})
		w := doJSON(h, http.MethodGet, "/graph?session_id=nope", nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})
}

func TestHandler_Cost(t *testing.T) {
	store := memory.New()
	ctx := context.Background()
	now := time.Now()
	if err := store.CreateSession(ctx, &storage.Session{ID: "s1", AgentID: "wf-agent", Status: "running", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	t.Run("404 for an unknown session", func(t *testing.T) {
		h := newTestHandler(store, nil)
		w := doJSON(h, http.MethodGet, "/cost?session_id=nope", nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})

	t.Run("501 without a cost tracker", func(t *testing.T) {
		h := newTestHandler(store, nil)
		w := doJSON(h, http.MethodGet, "/cost?session_id=s1", nil)
		if w.Code != http.StatusNotImplemented {
			t.Errorf("status = %d, want 501", w.Code)
		}
	})

	t.Run("reports the session's accumulated cost", func(t *testing.T) {
		ct := hooks.NewCostTracker(map[string]hooks.ModelPrice{
			"gpt-test": {PromptPricePerToken: 0.001, CompletionPricePerToken: 0.002},
		})
		_ = ct.After(context.Background(), &hooks.Event{
			Type: hooks.EventModelCallAfter,
			Name: "gpt-test",
			Metadata: map[string]any{
				"session_id":        "s1",
				"prompt_tokens":     10,
				"completion_tokens": 5,
			},
		})

		h := New(store, stream.NewBroker()).WithCostTracker(ct)
		w := doJSON(h, http.MethodGet, "/cost?session_id=s1", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
		}
		var report hooks.CostReport
		if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if report.TotalTokens != 15 {
			t.Errorf("total_tokens = %d, want 15", report.TotalTokens)
		}
	})

	// hooks.CostTracker is a plain session-id-keyed map with no tenant
	// awareness of its own — the handler must gate access via the
	// tenant-scoped session lookup, not the cost tracker.
	t.Run("cross-tenant session_id cannot read another tenant's cost", func(t *testing.T) {
		ctxA := storage.WithTenant(context.Background(), "tenant-a")
		if err := store.CreateSession(ctxA, &storage.Session{ID: "tenant-a-sess", AgentID: "wf-agent", Status: "running", CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		ct := hooks.NewCostTracker(map[string]hooks.ModelPrice{
			"gpt-test": {PromptPricePerToken: 0.001, CompletionPricePerToken: 0.002},
		})
		_ = ct.After(context.Background(), &hooks.Event{
			Type:     hooks.EventModelCallAfter,
			Name:     "gpt-test",
			Metadata: map[string]any{"session_id": "tenant-a-sess", "prompt_tokens": 1000, "completion_tokens": 1000},
		})

		h := New(store, stream.NewBroker()).WithCostTracker(ct)
		r := httptest.NewRequest(http.MethodGet, "/cost?session_id=tenant-a-sess", http.NoBody)
		r = r.WithContext(storage.WithTenant(context.Background(), "tenant-b"))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		if w.Code != http.StatusNotFound {
			t.Errorf("tenant-b read tenant-a's cost: status = %d, body=%s, want 404", w.Code, w.Body.String())
		}
	})
}

func TestHandler_Resume(t *testing.T) {
	store := memory.New()
	ctx := context.Background()
	now := time.Now()
	if err := store.CreateSession(ctx, &storage.Session{ID: "s1", AgentID: "hitl-agent", Status: "running", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	// Pause the run at the interrupt gate first, exactly as the application
	// would before a human uses the dashboard to resume it.
	runner := graph.NewRunner(hitlGraph(t), store)
	rs, err := runner.Run(ctx, "s1", graph.State{})
	if err != nil {
		t.Fatalf("initial run: %v", err)
	}
	if rs.Status != graph.RunStatusPaused {
		t.Fatalf("status = %s, want paused", rs.Status)
	}

	t.Run("400 with no session_id", func(t *testing.T) {
		h := newTestHandler(store, GraphRegistry{"hitl-agent": hitlGraph(t)})
		w := doJSON(h, http.MethodPost, "/resume", map[string]string{})
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("501 without a registered graph", func(t *testing.T) {
		h := newTestHandler(store, nil)
		w := doJSON(h, http.MethodPost, "/resume", map[string]string{"session_id": "s1"})
		if w.Code != http.StatusNotImplemented {
			t.Errorf("status = %d, want 501", w.Code)
		}
	})

	t.Run("404 for an unknown session", func(t *testing.T) {
		h := newTestHandler(store, GraphRegistry{"hitl-agent": hitlGraph(t)})
		w := doJSON(h, http.MethodPost, "/resume", map[string]string{"session_id": "nope"})
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})

	// os/server.go's handleDashboardAPI wraps every request body in
	// http.MaxBytesReader before delegating here; reproduce that so an
	// oversized body surfaces as 413, not a generic 400 "invalid JSON".
	t.Run("413 for an oversized body", func(t *testing.T) {
		h := newTestHandler(store, GraphRegistry{"hitl-agent": hitlGraph(t)})
		r := httptest.NewRequest(http.MethodPost, "/resume", strings.NewReader(`{"session_id":"s1"}`))
		w := httptest.NewRecorder()
		r.Body = http.MaxBytesReader(w, r.Body, 4)
		h.ServeHTTP(w, r)
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("status = %d, want 413 (body=%s)", w.Code, w.Body.String())
		}
	})

	t.Run("resumes past the gate to completion", func(t *testing.T) {
		h := newTestHandler(store, GraphRegistry{"hitl-agent": hitlGraph(t)})
		w := doJSON(h, http.MethodPost, "/resume", map[string]string{"session_id": "s1"})
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
		}
		var got graph.RunState
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Status != graph.RunStatusCompleted {
			t.Errorf("status = %s, want completed", got.Status)
		}
		if got.State["path"] != "preparegatefinish" {
			t.Errorf("path = %v, want preparegatefinish", got.State["path"])
		}
	})
}

func TestHandler_TimeTravel(t *testing.T) {
	store := memory.New()
	ctx := context.Background()
	now := time.Now()
	if err := store.CreateSession(ctx, &storage.Session{ID: "s1", AgentID: "wf-agent", Status: "running", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	runner := graph.NewRunner(linearGraph(t), store)
	if _, err := runner.Run(ctx, "s1", graph.State{}); err != nil {
		t.Fatalf("initial run: %v", err)
	}
	cps, err := store.ListCheckpoints(ctx, "s1")
	if err != nil || len(cps) == 0 {
		t.Fatalf("ListCheckpoints: %v (n=%d)", err, len(cps))
	}
	// The checkpoint recorded right after node "a" finished, which records "b"
	// as the next node to run — rewinding here should re-execute b then c.
	var midCP *storage.Checkpoint
	for _, cp := range cps {
		if cp.NodeID == "b" {
			midCP = cp
		}
	}
	if midCP == nil {
		t.Fatalf("no checkpoint found at node b among %d checkpoints", len(cps))
	}

	t.Run("400 with no checkpoint_id", func(t *testing.T) {
		h := newTestHandler(store, GraphRegistry{"wf-agent": linearGraph(t)})
		w := doJSON(h, http.MethodPost, "/timetravel", map[string]string{})
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("404 for an unknown checkpoint", func(t *testing.T) {
		h := newTestHandler(store, GraphRegistry{"wf-agent": linearGraph(t)})
		w := doJSON(h, http.MethodPost, "/timetravel", map[string]string{"checkpoint_id": "nope"})
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})

	t.Run("rewinds and re-executes from the checkpoint", func(t *testing.T) {
		h := newTestHandler(store, GraphRegistry{"wf-agent": linearGraph(t)})
		w := doJSON(h, http.MethodPost, "/timetravel", map[string]string{"checkpoint_id": midCP.ID})
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
		}
		var got graph.RunState
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Status != graph.RunStatusCompleted {
			t.Errorf("status = %s, want completed", got.Status)
		}
		// The checkpoint's own state already had "path" == "a"; rewinding from
		// node b re-executes b then c on top of it.
		if got.State["path"] != "abc" {
			t.Errorf("path = %v, want abc", got.State["path"])
		}
	})
}

// TestHandler_TenantIsolation proves a checkpoint/session created under one
// tenant is invisible to a request scoped to another, mirroring the guarantee
// storage.Storage documents and os/server_tenant_test.go verifies at the HTTP
// layer for the existing endpoints.
func TestHandler_TenantIsolation(t *testing.T) {
	store := memory.New()
	now := time.Now()
	ctxA := storage.WithTenant(context.Background(), "tenant-a")
	ctxB := storage.WithTenant(context.Background(), "tenant-b")

	if err := store.CreateSession(ctxA, &storage.Session{ID: "shared-id", AgentID: "wf-agent", Status: "running", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCheckpoint(ctxA, &storage.Checkpoint{ID: "cp-a", SessionID: "shared-id", RunID: "r", NodeID: "b", State: map[string]any{}, SeqNum: 1, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	h := newTestHandler(store, nil)
	r := httptest.NewRequest(http.MethodGet, "/checkpoints?session_id=shared-id", http.NoBody)
	r = r.WithContext(ctxB)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Checkpoints []*storage.Checkpoint `json:"checkpoints"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Checkpoints) != 0 {
		t.Errorf("tenant-b saw %d of tenant-a's checkpoints, want 0", len(resp.Checkpoints))
	}
}

// TestHandler_StartRun proves the gap this handler closes: before it existed,
// nothing in ChronosOS's HTTP surface could start a brand-new run — only
// resume/time-travel an already-existing session created by some other
// in-process caller. A POST to /runs with just an agent_id must create the
// session itself and drive it to its first pause/completion.
func TestHandler_StartRun(t *testing.T) {
	t.Run("400 with no agent_id", func(t *testing.T) {
		h := newTestHandler(memory.New(), GraphRegistry{"wf-agent": linearGraph(t)})
		w := doJSON(h, http.MethodPost, "/runs", map[string]string{})
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})

	t.Run("501 without a registered graph", func(t *testing.T) {
		h := newTestHandler(memory.New(), nil)
		w := doJSON(h, http.MethodPost, "/runs", map[string]string{"agent_id": "wf-agent"})
		if w.Code != http.StatusNotImplemented {
			t.Errorf("status = %d, want 501", w.Code)
		}
	})

	t.Run("method not allowed for GET", func(t *testing.T) {
		h := newTestHandler(memory.New(), GraphRegistry{"wf-agent": linearGraph(t)})
		w := doJSON(h, http.MethodGet, "/runs", nil)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want 405", w.Code)
		}
	})

	t.Run("starts a run through completion, generating a session id", func(t *testing.T) {
		store := memory.New()
		h := newTestHandler(store, GraphRegistry{"wf-agent": linearGraph(t)})
		w := doJSON(h, http.MethodPost, "/runs", map[string]any{"agent_id": "wf-agent"})
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
		}
		var got graph.RunState
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Status != graph.RunStatusCompleted {
			t.Errorf("status = %s, want completed", got.Status)
		}
		if got.State["path"] != "abc" {
			t.Errorf("path = %v, want abc", got.State["path"])
		}
		if got.SessionID == "" {
			t.Error("expected a generated session_id")
		}
		sess, err := store.GetSession(context.Background(), got.SessionID)
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if sess.AgentID != "wf-agent" {
			t.Errorf("session AgentID = %q, want wf-agent", sess.AgentID)
		}
	})

	t.Run("input seeds initial state; a client-supplied session_id is ignored", func(t *testing.T) {
		store := memory.New()
		h := newTestHandler(store, GraphRegistry{"hitl-agent": hitlGraph(t)})
		w := doJSON(h, http.MethodPost, "/runs", map[string]any{
			"agent_id":   "hitl-agent",
			"session_id": "attacker-chosen-id", // must not reach the store — see handleStartRun's doc comment
			"input":      map[string]any{"seed": "x"},
		})
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
		}
		var got graph.RunState
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.SessionID == "attacker-chosen-id" {
			t.Error("session id must always be server-generated, never the caller-supplied session_id")
		}
		if got.Status != graph.RunStatusPaused {
			t.Errorf("status = %s, want paused (hitl gate)", got.Status)
		}
		if got.State["seed"] != "x" {
			t.Errorf("seed = %v, want x (input should seed initial state)", got.State["seed"])
		}
	})

	// TestHandler_StartRun_TenantScoping (below) covers cross-tenant isolation
	// for the created session; there is no caller-supplied session_id to
	// probe existence with any more (see the test above), which is the fix
	// for the cross-tenant existence oracle a caller-chosen id would allow.
}

// TestHandler_StartRun_TenantScoping proves a session created via handleStartRun
// is only visible under the tenant that created it.
func TestHandler_StartRun_TenantScoping(t *testing.T) {
	store := memory.New()
	h := newTestHandler(store, GraphRegistry{"wf-agent": linearGraph(t)})
	body, _ := json.Marshal(map[string]any{"agent_id": "wf-agent"})
	r := httptest.NewRequest(http.MethodPost, "/runs", bytes.NewReader(body))
	r = r.WithContext(storage.WithTenant(context.Background(), "tenant-a"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var got graph.RunState
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}

	if _, err := store.GetSession(storage.WithTenant(context.Background(), "tenant-b"), got.SessionID); err == nil {
		t.Error("expected tenant-b to not see tenant-a's newly created session")
	}
	if _, err := store.GetSession(storage.WithTenant(context.Background(), "tenant-a"), got.SessionID); err != nil {
		t.Errorf("expected tenant-a to see its own session: %v", err)
	}
}
