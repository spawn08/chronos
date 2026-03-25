package cmd

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

func TestExecute_InteractiveAlias_Squeeze(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stdin/signal tests skipped on windows")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("CHRONOS_DB_PATH", filepath.Join(tmp, "r.db"))

	oldIn := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldIn
		_ = r.Close()
	})
	go func() {
		_, _ = w.WriteString("/help\n/quit\n")
		_ = w.Close()
	}()

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"chronos", "interactive"}

	_ = captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Errorf("Execute: %v", err)
		}
	})
}

func TestExecute_Monitor_MockEndpoint_SIGINT_Squeeze(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGINT not used on windows")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"sessions":[]}`))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("# test\n"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"chronos", "monitor", "--endpoint", srv.URL, "--interval", "1"}

	done := make(chan error, 1)
	go func() { done <- runMonitor() }()

	time.Sleep(400 * time.Millisecond)
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGINT)

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("runMonitor: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("runMonitor did not stop")
	}
}

func TestExecute_Serve_TMPDB_SIGINT_Squeeze(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGINT not used on windows")
	}
	tmp := t.TempDir()
	db := filepath.Join(tmp, "os.db")
	t.Setenv("CHRONOS_DB_PATH", db)

	addr := "127.0.0.1:18765"
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Skipf("port busy: %v", err)
	}
	_ = ln.Close()

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"chronos", "serve", addr}

	done := make(chan error, 1)
	go func() { done <- runServe() }()

	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://" + addr + "/health")
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	_ = syscall.Kill(syscall.Getpid(), syscall.SIGINT)

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("runServe: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("runServe did not stop")
	}
}

func TestExecute_Run_WithMockModel_Squeeze(t *testing.T) {
	chatBody := `{"id":"r1","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"run-ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(chatBody))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "agents.yaml")
	yaml := fmt.Sprintf(`agents:
  - id: run-agent
    name: RunAgent
    model:
      provider: compatible
      model: test-model
      base_url: %q
      api_key: test-key
`, srv.URL)
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHRONOS_CONFIG", cfgPath)
	t.Setenv("CHRONOS_DB_PATH", filepath.Join(tmp, "r2.db"))

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"chronos", "run", "--agent", "run-agent", "ping"}

	out := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Errorf("Execute: %v", err)
		}
	})
	if !strings.Contains(out, "run-ok") || !strings.Contains(out, "tokens") {
		t.Fatalf("unexpected output: %q", out[:min(500, len(out))])
	}
}

func TestExecute_Pipe_WithMockModel_Squeeze(t *testing.T) {
	chatBody := `{"id":"r1","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"piped-ok"}}],"usage":{"prompt_tokens":2,"completion_tokens":3}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(chatBody))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "agents.yaml")
	yaml := fmt.Sprintf(`agents:
  - id: pipe-agent
    name: PipeAgent
    model:
      provider: compatible
      model: test-model
      base_url: %q
      api_key: test-key
`, srv.URL)
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHRONOS_CONFIG", cfgPath)
	t.Setenv("CHRONOS_DB_PATH", filepath.Join(tmp, "p.db"))

	oldIn := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldIn
		_ = r.Close()
	})
	go func() {
		_, _ = w.WriteString("hello\n")
		_ = w.Close()
	}()

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"chronos", "pipe"}

	out := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Errorf("Execute: %v", err)
		}
	})
	if !strings.Contains(out, "piped-ok") {
		t.Fatalf("expected piped response in stdout, got: %q", out[:min(400, len(out))])
	}
}

func TestExecute_Pipe_SecondLineChatError_Squeeze(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if n.Add(1) == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"r1","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"first"}}],"usage":{}}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"fail"}`))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "agents.yaml")
	yaml := fmt.Sprintf(`agents:
  - id: pe
    name: PE
    model:
      provider: compatible
      model: m
      base_url: %q
      api_key: k
`, srv.URL)
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHRONOS_CONFIG", cfgPath)
	t.Setenv("CHRONOS_DB_PATH", filepath.Join(tmp, "pe.db"))

	oldIn := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldIn
		_ = r.Close()
	})
	go func() {
		_, _ = w.WriteString("ok\nfail-line\n")
		_ = w.Close()
	}()

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"chronos", "pipe"}

	out := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Errorf("Execute: %v", err)
		}
	})
	if !strings.Contains(out, "first") || !strings.Contains(out, "error") {
		t.Fatalf("stdout: %q", out[:min(500, len(out))])
	}
}

func TestExecute_TeamShow_Squeeze(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "agents.yaml")
	yaml := fmt.Sprintf(`agents:
  - id: x1
    name: X1
    model:
      provider: compatible
      model: m
      base_url: %q
      api_key: k
teams:
  - id: tshow
    name: TShow
    strategy: coordinator
    agents: [x1]
    coordinator: x1
    max_iterations: 5
    error_strategy: collect
`, "http://127.0.0.1:9")
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHRONOS_CONFIG", cfgPath)

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"chronos", "team", "show", "tshow"}

	out := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	for _, needle := range []string{"tshow", "coordinator", "Max Iterations", "Error Strategy"} {
		if !strings.Contains(out, needle) {
			t.Errorf("missing %q in %q", needle, out[:min(500, len(out))])
		}
	}
}

func TestExecute_TeamRun_ParallelSuccess_Squeeze(t *testing.T) {
	body := `{"id":"r","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"p"}}],"usage":{}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "agents.yaml")
	yaml := fmt.Sprintf(`agents:
  - id: pa
    name: PA
    model:
      provider: compatible
      model: m
      base_url: %q
      api_key: k
  - id: pb
    name: PB
    model:
      provider: compatible
      model: m
      base_url: %q
      api_key: k
teams:
  - id: par
    name: Par
    strategy: parallel
    agents: [pa, pb]
    max_concurrency: 2
`, srv.URL, srv.URL)
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHRONOS_CONFIG", cfgPath)
	t.Setenv("CHRONOS_DB_PATH", filepath.Join(tmp, "par.db"))

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"chronos", "team", "run", "par", "go"}

	out := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Errorf("Execute: %v", err)
		}
	})
	if !strings.Contains(out, "parallel") {
		t.Fatalf("output: %q", out[:min(500, len(out))])
	}
}

func TestExecute_TeamRun_SequentialSuccess_Squeeze(t *testing.T) {
	body := `{"id":"r","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"step"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "agents.yaml")
	yaml := fmt.Sprintf(`agents:
  - id: ta
    name: A
    model:
      provider: compatible
      model: m
      base_url: %q
      api_key: k
  - id: tb
    name: B
    model:
      provider: compatible
      model: m
      base_url: %q
      api_key: k
teams:
  - id: duo
    name: Duo
    strategy: sequential
    agents: [ta, tb]
`, srv.URL, srv.URL)
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHRONOS_CONFIG", cfgPath)
	t.Setenv("CHRONOS_DB_PATH", filepath.Join(tmp, "team.db"))

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"chronos", "team", "run", "duo", "hello team"}

	out := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Errorf("Execute: %v", err)
		}
	})
	if !strings.Contains(out, "Team:") || !strings.Contains(out, "sequential") {
		t.Fatalf("output: %q", out[:min(600, len(out))])
	}
}

func TestExecute_Pipe_NoConfig_Squeeze(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CHRONOS_CONFIG", filepath.Join(tmp, "missing.yaml"))
	t.Setenv("CHRONOS_DB_PATH", filepath.Join(tmp, "x.db"))

	oldIn := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	_ = w.Close()
	t.Cleanup(func() {
		os.Stdin = oldIn
		_ = r.Close()
	})

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"chronos", "pipe"}

	err := Execute()
	if err == nil {
		t.Fatal("expected error loading agent for pipe")
	}
}

func TestExecute_Run_NoAgentConfig_Squeeze(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CHRONOS_CONFIG", filepath.Join(tmp, "none.yaml"))
	t.Setenv("CHRONOS_DB_PATH", filepath.Join(tmp, "y.db"))

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"chronos", "run", "hello world"}

	out := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Errorf("Execute: %v", err)
		}
	})
	if !strings.Contains(out, "Message:") || !strings.Contains(out, "agents.yaml") {
		t.Fatalf("unexpected output: %q", out[:min(300, len(out))])
	}
}

func TestOpenStore_InvalidPath_Squeeze(t *testing.T) {
	// SQLite cannot open a directory as a database file.
	dir := t.TempDir()
	t.Setenv("CHRONOS_DB_PATH", dir)
	_, err := openStore()
	if err == nil {
		t.Fatal("expected error when db path is a directory")
	}
}

func TestOpenStore_CustomDBPath_Squeeze(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "custom.db")
	t.Setenv("CHRONOS_DB_PATH", p)
	st, err := openStore()
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	st.Close()
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("db file: %v", err)
	}
}

func TestAgentList_ModelDefaultAndLongDesc_Squeeze(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "agents.yaml")
	longDesc := strings.Repeat("d", 40)
	content := fmt.Sprintf(`agents:
  - id: a1
    name: N1
    model:
      provider: openai
    description: %q
`, longDesc)
	if err := os.WriteFile(cfg, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHRONOS_CONFIG", cfg)

	_ = captureStdout(t, func() {
		if err := agentList(); err != nil {
			t.Fatalf("agentList: %v", err)
		}
	})
}

func TestAgentShow_Branches_Squeeze(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "agents.yaml")
	sys := strings.Repeat("s", 100)
	content := fmt.Sprintf(`agents:
  - id: show1
    name: ShowAgent
    model:
      provider: openai
      model: gpt-4o
      base_url: https://example.com/v1
    storage:
      backend: postgres
      dsn: %q
    system_prompt: %q
    instructions: ["do X"]
    capabilities: ["c1"]
    sub_agents: ["child"]
    stream: true
`, strings.Repeat("p", 50), sys)
	if err := os.WriteFile(cfg, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHRONOS_CONFIG", cfg)

	out := captureStdout(t, func() {
		if err := agentShow("show1"); err != nil {
			t.Fatalf("agentShow: %v", err)
		}
	})
	for _, needle := range []string{"Base URL", "System Prompt", "Instructions", "Capabilities", "Sub-agents", "Stream"} {
		if !strings.Contains(out, needle) {
			t.Errorf("output missing %q", needle)
		}
	}
}

func TestEvalList_WithGlobMatch_Squeeze(t *testing.T) {
	tmp := t.TempDir()
	ev := filepath.Join(tmp, "evals")
	if err := os.MkdirAll(ev, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ev, "suite.yaml"), []byte("suite: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldWD, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	_ = captureStdout(t, func() {
		if err := evalList(); err != nil {
			t.Fatalf("evalList: %v", err)
		}
	})
}

func TestSessionsList_WithAgentFilterArg_Squeeze(t *testing.T) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now()
	_ = store.CreateSession(ctx, &storage.Session{ID: "sx", AgentID: "filter-me", Status: "done", CreatedAt: now, UpdatedAt: now})

	_ = captureStdout(t, func() {
		if err := sessionsList(ctx, store, "filter-me"); err != nil {
			t.Fatalf("sessionsList: %v", err)
		}
	})
}

func TestSessionsExport_GetSessionError_Squeeze(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	err := sessionsExport(ctx, store, "does-not-exist")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMemoryList_StoreClosed_Squeeze(t *testing.T) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Migrate(context.Background())
	store.Close()

	err = memoryList(context.Background(), store, "any")
	if err == nil {
		t.Fatal("expected error from closed store")
	}
}

func TestRunAgentCmd_UnknownSubcommand_Squeeze(t *testing.T) {
	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"chronos", "agent", "nope"}
	err := runAgentCmd()
	if err == nil || !strings.Contains(err.Error(), "unknown agent subcommand") {
		t.Fatalf("err=%v", err)
	}
}

func TestTeamRun_StrategyError_Squeeze(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "agents.yaml")
	yaml := `agents:
  - id: a1
    name: A1
    model:
      provider: openai
      model: m
      api_key: k
teams:
  - id: tbad
    name: TB
    strategy: not-a-real-strategy
    agents: [a1]
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHRONOS_CONFIG", cfgPath)

	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"chronos", "team", "run", "tbad", "hi"}

	err := Execute()
	if err == nil || !strings.Contains(err.Error(), "strategy") {
		t.Fatalf("err=%v", err)
	}
}

func TestTeamRun_UnknownAgentInTeam_Squeeze(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "agents.yaml")
	yaml := `agents:
  - id: a1
    name: A1
    model:
      provider: openai
      model: m
      api_key: k
teams:
  - id: t1
    name: T1
    strategy: sequential
    agents: [a1, missing-agent]
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHRONOS_CONFIG", cfgPath)

	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"chronos", "team", "run", "t1", "task"}

	err := Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown agent") {
		t.Fatalf("err=%v", err)
	}
}

func TestTeamRun_BadErrorStrategy_Squeeze(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "agents.yaml")
	yaml := `agents:
  - id: a1
    name: A1
    model:
      provider: openai
      model: m
      api_key: k
  - id: a2
    name: A2
    model:
      provider: openai
      model: m
      api_key: k
teams:
  - id: t1
    name: T1
    strategy: sequential
    agents: [a1, a2]
    error_strategy: nonsense
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHRONOS_CONFIG", cfgPath)

	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"chronos", "team", "run", "t1", "task"}

	err := Execute()
	if err == nil || !strings.Contains(err.Error(), "error strategy") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunConfig_ShowWithAgentsFile_Squeeze(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "agents.yaml")
	if err := os.WriteFile(cfgPath, []byte(`agents:
  - id: z1
    name: Z
    model:
      provider: openai
      model: gpt-4o
      api_key: x
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHRONOS_CONFIG", cfgPath)

	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"chronos", "config", "show"}

	out := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(out, "z1") {
		t.Fatalf("expected agent id in output: %q", out[:min(500, len(out))])
	}
}

func TestExecute_EvalRun_Squeeze(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "suite.yaml")
	if err := os.WriteFile(f, []byte("suite: test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"chronos", "eval", "run", f}

	out := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(out, "Eval suite") {
		t.Fatalf("output: %q", out)
	}
}

func TestExecute_DB_Init_Squeeze(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CHRONOS_DB_PATH", filepath.Join(tmp, "init.db"))

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"chronos", "db", "init"}

	out := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(out, "initialized") {
		t.Fatalf("output: %q", out)
	}
}

func TestExecute_MemoryForget_Squeeze(t *testing.T) {
	tmp := t.TempDir()
	dbp := filepath.Join(tmp, "mf.db")
	t.Setenv("CHRONOS_DB_PATH", dbp)
	st, err := sqlite.New(dbp)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_ = st.Migrate(ctx)
	now := time.Now()
	_ = st.PutMemory(ctx, &storage.MemoryRecord{
		ID: "delme", AgentID: "ag2", Kind: "long_term", Key: "x", Value: 1, CreatedAt: now,
	})
	st.Close()

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"chronos", "memory", "forget", "delme"}

	if err := Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestExecute_MemoryClear_Squeeze(t *testing.T) {
	tmp := t.TempDir()
	dbp := filepath.Join(tmp, "mem.db")
	t.Setenv("CHRONOS_DB_PATH", dbp)
	st, err := sqlite.New(dbp)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_ = st.Migrate(ctx)
	now := time.Now()
	_ = st.PutMemory(ctx, &storage.MemoryRecord{
		ID: "mid", AgentID: "ag1", Kind: "long_term", Key: "k", Value: "v", CreatedAt: now,
	})
	st.Close()

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"chronos", "memory", "clear", "ag1"}

	out := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(out, "Clearing") || !strings.Contains(out, "Cleared 1") {
		t.Fatalf("output: %q", out)
	}
}

func TestExecute_DB_Status_Squeeze(t *testing.T) {
	tmp := t.TempDir()
	dbp := filepath.Join(tmp, "stat.db")
	t.Setenv("CHRONOS_DB_PATH", dbp)
	st, err := sqlite.New(dbp)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Migrate(context.Background())
	st.Close()

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"chronos", "db", "status"}

	out := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(out, "Database:") || !strings.Contains(out, "Sessions:") {
		t.Fatalf("output: %q", out[:min(400, len(out))])
	}
}

func TestRunEvalCmd_UnknownSubcommand_Squeeze(t *testing.T) {
	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"chronos", "eval", "bogus"}
	err := Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown eval subcommand") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunSessions_UnknownSubcommand_Squeeze(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CHRONOS_DB_PATH", filepath.Join(tmp, "s.db"))

	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"chronos", "sessions", "nope"}

	err := Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown sessions subcommand") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunMemory_UnknownSubcommand_Squeeze(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CHRONOS_DB_PATH", filepath.Join(tmp, "m.db"))

	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"chronos", "memory", "nope"}

	err := Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown memory subcommand") {
		t.Fatalf("err=%v", err)
	}
}
