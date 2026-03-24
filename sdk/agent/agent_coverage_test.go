package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/mcp"
	"github.com/spawn08/chronos/storage"
)

func TestConnectMCP_ConnectError(t *testing.T) {
	cli, err := mcp.NewClient(mcp.ServerConfig{Name: "bad", Command: "/nonexistent/mcp/server/binary"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	a, _ := New("a", "a").Build()
	a.MCPClients = []*mcp.Client{cli}

	if err := a.ConnectMCP(context.Background()); err == nil {
		t.Fatal("expected connect error")
	}
}

func TestConnectMCP_NilClientPanics(t *testing.T) {
	a, _ := New("a", "a").Build()
	a.MCPClients = []*mcp.Client{nil}

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on nil MCP client")
		}
	}()
	_ = a.ConnectMCP(context.Background())
}

func TestCloseMCP_NoClients(t *testing.T) {
	a, _ := New("a", "a").Build()
	a.MCPClients = nil
	a.CloseMCP() // must not panic
}

func TestChatWithSession_PersistAssistantError(t *testing.T) {
	base := newTestStorage()
	st := &failAfterUserAppend{testStorage: base, failAfter: 1}

	a, _ := New("a", "a").
		WithModel(&testProvider{response: &model.ChatResponse{Content: "hi"}}).
		WithStorage(st).
		Build()

	_, err := a.ChatWithSession(context.Background(), "sess1", "hello")
	if err == nil {
		t.Fatal("expected error persisting assistant message")
	}
}

func TestChatWithSession_EmptyUserMessage(t *testing.T) {
	st := newTestStorage()
	a, _ := New("a", "a").
		WithModel(&testProvider{response: &model.ChatResponse{Content: "ack"}}).
		WithStorage(st).
		Build()

	resp, err := a.ChatWithSession(context.Background(), "empty-msg-sess", "")
	if err != nil {
		t.Fatalf("ChatWithSession: %v", err)
	}
	if resp == nil || resp.Content != "ack" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestBuildAgent_InvalidProvider(t *testing.T) {
	_, err := BuildAgent(context.Background(), &AgentConfig{
		ID:   "x",
		Name: "x",
		Model: ModelConfig{
			Provider: "totally-unknown-provider-xyz",
			Model:    "m",
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildAgent_InvalidStorageBackend(t *testing.T) {
	_, err := BuildAgent(context.Background(), &AgentConfig{
		ID:   "x",
		Name: "x",
		Model: ModelConfig{
			Provider: "openai",
			Model:    "gpt-4o",
		},
		Storage: StorageConfig{Backend: "not-sqlite"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildAgent_PostgresWithoutDSN(t *testing.T) {
	_, err := BuildAgent(context.Background(), &AgentConfig{
		ID:   "x",
		Name: "x",
		Model: ModelConfig{
			Provider: "openai",
			Model:    "gpt-4o",
		},
		Storage: StorageConfig{Backend: "postgres"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRun_NoModelNoGraph(t *testing.T) {
	a, _ := New("n", "n").Build()

	_, err := a.Run(context.Background(), map[string]any{"message": "hi"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRun_ModelOnlySuccess(t *testing.T) {
	a, _ := New("m", "m").
		WithModel(&testProvider{response: &model.ChatResponse{Content: "ok"}}).
		Build()

	rs, err := a.Run(context.Background(), map[string]any{"message": "hi"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rs.Status != graph.RunStatusCompleted {
		t.Fatalf("status=%v", rs.Status)
	}
}

func TestRun_CreateSessionError(t *testing.T) {
	g := graph.New("t")
	g.AddNode("n1", func(ctx context.Context, s graph.State) (graph.State, error) {
		return s, nil
	})
	g.SetEntryPoint("n1")
	g.SetFinishPoint("n1")
	cg, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	st := &failCreateSessionStore{testStorage: newTestStorage()}

	a, _ := New("g", "g").
		WithGraph(g).
		WithStorage(st).
		Build()
	a.Graph = cg

	_, err = a.Run(context.Background(), map[string]any{"message": "hi"})
	if err == nil {
		t.Fatal("expected CreateSession error")
	}
}

// failAfterUserAppend delegates to testStorage but fails AppendEvent after N successful appends.
type failAfterUserAppend struct {
	*testStorage
	appends   int
	failAfter int
}

func (f *failAfterUserAppend) AppendEvent(ctx context.Context, e *storage.Event) error {
	f.appends++
	if f.appends > f.failAfter {
		return errors.New("append failed")
	}
	return f.testStorage.AppendEvent(ctx, e)
}

type failCreateSessionStore struct {
	*testStorage
}

func (f *failCreateSessionStore) CreateSession(ctx context.Context, sess *storage.Session) error {
	return errors.New("cannot create session")
}
