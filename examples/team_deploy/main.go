// Example: team_deploy — Deploy multi-agent teams from YAML with sandbox isolation.
//
// This example shows how to:
//   - Load a team deployment config from YAML
//   - Build agents with tools defined in YAML
//   - Run a team in a sandboxed process environment
//   - Use both sequential pipeline and coordinator strategies
//
// What you'll learn:
//   - YAML-driven agent and team configuration
//   - Sandbox-backed tool execution for safe agent autonomy
//   - Sequential vs. coordinator team strategies
//   - Deploying a full coding team from config
//
// Prerequisites:
//   - Go 1.22+
//   - Set OPENAI_API_KEY for real LLM responses (mock fallback available)
//
// Run:
//
//	go run ./examples/team_deploy/
//
// Or via CLI:
//
//	go run ./cli/main.go deploy examples/team_deploy/deploy.yaml "Add error handling to the API"
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool/builtins"
	"github.com/spawn08/chronos/sandbox"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/team"
)

func main() {
	ctx := context.Background()

	fmt.Println("╔═══════════════════════════════════════════════════╗")
	fmt.Println("║    Chronos Team Deploy Example                    ║")
	fmt.Println("╚═══════════════════════════════════════════════════╝")

	// ════════════════════════════════════════════════════════════════
	// Step 1: Create a sandbox environment
	//
	// The sandbox isolates agent command execution. Agents can run
	// shell commands, but only within the sandbox boundary.
	// ProcessSandbox runs commands as subprocesses in a work directory.
	// For production, use ContainerSandbox (Docker) or K8sJobSandbox.
	// ════════════════════════════════════════════════════════════════
	fmt.Println("\n━━━ Step 1: Sandbox Environment ━━━")

	workDir := os.TempDir() + "/chronos-team-demo"
	_ = os.MkdirAll(workDir, 0o755)
	defer os.RemoveAll(workDir)

	sb := sandbox.NewProcessSandbox(workDir)
	defer sb.Close()

	fmt.Printf("  Backend:  process\n")
	fmt.Printf("  Work dir: %s\n", workDir)

	// ════════════════════════════════════════════════════════════════
	// Step 2: Build agents programmatically with sandbox tools
	//
	// Each agent gets a role-specific system prompt and a set of tools.
	// The shell tool is sandbox-backed — commands run inside the sandbox.
	// File tools operate on the sandbox work directory.
	// ════════════════════════════════════════════════════════════════
	fmt.Println("\n━━━ Step 2: Building Agents ━━━")

	provider := resolveProvider()

	architect := buildCodingAgent("architect", "Software Architect",
		"Designs architecture and task breakdown",
		[]string{"architecture", "planning"},
		provider, sb, workDir,
		"You are a software architect. Break tasks into specific, implementable steps. Output a JSON plan with tasks, dependencies, and acceptance criteria.")

	implementer := buildCodingAgent("implementer", "Backend Developer",
		"Implements code following the plan",
		[]string{"backend", "golang"},
		provider, sb, workDir,
		"You are an expert Go developer. Implement the given task following Go conventions. Use file_read to understand existing code, file_write to make changes, and shell to run builds.")

	tester := buildCodingAgent("tester", "Test Engineer",
		"Writes and runs tests",
		[]string{"testing", "quality"},
		provider, sb, workDir,
		"You are a test engineer. Write table-driven Go tests for the given implementation. Run tests with shell('go test -v ./...').")

	reviewer := buildCodingAgent("reviewer", "Code Reviewer",
		"Reviews code for quality and security",
		[]string{"code-review", "security"},
		provider, sb, workDir,
		"You are a code reviewer. Review changes for correctness, security, performance, and maintainability. Format: ✅ Good / ⚠️ Suggestions / 🚨 Must fix.")

	fmt.Printf("  Built 4 agents: architect, implementer, tester, reviewer\n")

	// ════════════════════════════════════════════════════════════════
	// Step 3: Run a sequential pipeline team
	//
	// The sequential strategy creates a pipeline: each agent receives
	// the output of the previous agent as context. The architect plans,
	// the implementer codes, the tester validates, the reviewer approves.
	// ════════════════════════════════════════════════════════════════
	fmt.Println("\n━━━ Step 3: Sequential Pipeline Team ━━━")

	pipeline := team.New("dev-pipeline", "Development Pipeline", team.StrategySequential).
		AddAgent(architect).
		AddAgent(implementer).
		AddAgent(tester).
		AddAgent(reviewer)

	task := "Create a health check endpoint that returns the service version and uptime"
	fmt.Printf("  Task: %s\n", task)

	result, err := pipeline.Run(ctx, graph.State{"message": task})
	if err != nil {
		log.Printf("  Pipeline error: %v (expected with mock provider)", err)
	} else {
		if resp, ok := result["response"]; ok {
			fmt.Printf("  Pipeline result: %s\n", truncate(fmt.Sprintf("%v", resp), 200))
		}
		fmt.Printf("  Messages exchanged: %d\n", len(pipeline.MessageHistory()))
	}

	// ════════════════════════════════════════════════════════════════
	// Step 4: Run a coordinator team
	//
	// The coordinator strategy uses an LLM (the architect) to decompose
	// the task into sub-tasks, then delegates each sub-task to the
	// appropriate agent. Independent tasks can run in parallel.
	// ════════════════════════════════════════════════════════════════
	fmt.Println("\n━━━ Step 4: Coordinator Team ━━━")

	coordTeam := team.New("dev-coordinated", "Coordinated Dev Team", team.StrategyCoordinator).
		SetCoordinator(architect).
		AddAgent(implementer).
		AddAgent(tester).
		AddAgent(reviewer).
		SetMaxIterations(2)

	coordTask := "Add input validation to ensure email addresses are valid"
	fmt.Printf("  Task: %s\n", coordTask)

	result, err = coordTeam.Run(ctx, graph.State{"message": coordTask})
	if err != nil {
		fmt.Printf("  Coordinator: %v (expected with mock provider)\n", err)
	} else {
		fmt.Printf("  Coordinator completed with %d state keys\n", len(result))
		fmt.Printf("  Messages exchanged: %d\n", len(coordTeam.MessageHistory()))
	}

	// ════════════════════════════════════════════════════════════════
	// Step 5: Run a parallel review team
	//
	// The parallel strategy runs multiple agents concurrently.
	// Here tester and reviewer both analyze the same code simultaneously.
	// Results are merged — you get both test results and review feedback.
	// ════════════════════════════════════════════════════════════════
	fmt.Println("\n━━━ Step 5: Parallel Review ━━━")

	parallelTeam := team.New("parallel-review", "Parallel Review", team.StrategyParallel).
		AddAgent(tester).
		AddAgent(reviewer).
		SetMaxConcurrency(2).
		SetErrorStrategy(team.ErrorStrategyBestEffort)

	result, err = parallelTeam.Run(ctx, graph.State{
		"message": "Review this code: func Add(a, b int) int { return a + b }",
	})
	if err != nil {
		fmt.Printf("  Parallel error: %v\n", err)
	} else {
		for k, v := range result {
			if strings.HasPrefix(k, "_") {
				continue
			}
			fmt.Printf("  %s: %s\n", k, truncate(fmt.Sprintf("%v", v), 120))
		}
	}

	fmt.Println("\n✓ Team deployment example completed.")
	fmt.Println("\nTo deploy via CLI:")
	fmt.Println("  go run ./cli/main.go deploy examples/team_deploy/deploy.yaml \"Your task here\"")
}

// buildCodingAgent creates an agent with file and sandbox-backed shell tools.
func buildCodingAgent(id, name, desc string, caps []string, provider model.Provider, sb sandbox.Sandbox, workDir, systemPrompt string) *agent.Agent {
	b := agent.New(id, name).
		Description(desc).
		WithModel(provider).
		WithSystemPrompt(systemPrompt).
		AddToolkit(builtins.NewFileToolkit(workDir)).
		AddTool(builtins.NewSandboxShellTool(sb, 0))

	for _, c := range caps {
		b.AddCapability(c)
	}

	a, err := b.Build()
	if err != nil {
		log.Fatalf("build agent %s: %v", id, err)
	}
	return a
}

func resolveProvider() model.Provider {
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return model.NewOpenAI(key)
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return model.NewAnthropic(key)
	}
	fmt.Println("  ⚠ No API key found, using mock provider")
	return &mockProvider{}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

type mockProvider struct{}

func (m *mockProvider) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	last := req.Messages[len(req.Messages)-1].Content
	if strings.Contains(last, "Analyze the following") || strings.Contains(last, "Break") {
		plan := `{"tasks": [{"agent_id": "implementer", "description": "Implement the endpoint"}, {"agent_id": "tester", "description": "Write tests", "depends_on": "implementer"}], "done": false}`
		return &model.ChatResponse{Content: plan, Role: "assistant", StopReason: model.StopReasonEnd}, nil
	}
	if strings.Contains(last, "Review") || strings.Contains(last, "Iteration") || strings.Contains(last, "done") {
		return &model.ChatResponse{Content: `{"tasks": [], "done": true}`, Role: "assistant", StopReason: model.StopReasonEnd}, nil
	}
	return &model.ChatResponse{
		Content:    fmt.Sprintf("[%s processed: %.80s]", req.Messages[0].Content[:min(20, len(req.Messages[0].Content))], last),
		Role:       "assistant",
		StopReason: model.StopReasonEnd,
	}, nil
}

func (m *mockProvider) StreamChat(_ context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	ch := make(chan *model.ChatResponse, 1)
	resp, _ := m.Chat(context.Background(), req)
	ch <- resp
	close(ch)
	return ch, nil
}

func (m *mockProvider) Name() string  { return "mock" }
func (m *mockProvider) Model() string { return "mock-v1" }
