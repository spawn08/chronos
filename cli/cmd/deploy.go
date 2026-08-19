package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/tool/builtins"
	"github.com/spawn08/chronos/sandbox"
	"github.com/spawn08/chronos/sdk/agent"
)

func runDeploy() error {
	args := os.Args[2:]
	if len(args) < 2 {
		return fmt.Errorf("usage: chronos deploy <config.yaml> <message>")
	}
	configPath := args[0]
	message := strings.Join(args[1:], " ")

	// LoadFile parses the YAML, applies defaults, expands ${ENV}, promotes
	// legacy top-level name/sandbox into deployment, and validates — the same
	// pipeline every other command uses.
	fc, err := agent.LoadFile(configPath)
	if err != nil {
		return fmt.Errorf("load deploy config: %w", err)
	}
	if fc.Deployment == nil {
		return fmt.Errorf("deploy: config %q is missing the required `deployment:` block (with `name:` and `sandbox:`)", configPath)
	}
	dep := fc.Deployment

	fmt.Println("╔═══════════════════════════════════════════════════╗")
	fmt.Println("║           Chronos Deploy                         ║")
	fmt.Println("╚═══════════════════════════════════════════════════╝")
	fmt.Printf("  Deployment: %s\n", dep.Name)
	fmt.Printf("  Sandbox:    %s\n", dep.Sandbox.Backend)
	fmt.Printf("  Agents:     %d\n", len(fc.Agents))
	fmt.Printf("  Teams:      %d\n", len(fc.Teams))
	fmt.Printf("  Message:    %s\n\n", message)

	ctx := context.Background()

	timeout := 5 * time.Minute
	if dep.Sandbox.Timeout != "" {
		if td, tdErr := time.ParseDuration(dep.Sandbox.Timeout); tdErr == nil {
			timeout = td
		}
	}

	sb, err := sandbox.NewFromConfig(sandbox.Config{
		Backend: sandbox.ParseBackend(dep.Sandbox.Backend),
		WorkDir: dep.Sandbox.WorkDir,
		Image:   dep.Sandbox.Image,
		Network: dep.Sandbox.Network,
	})
	if err != nil {
		return fmt.Errorf("create sandbox: %w", err)
	}
	defer sb.Close()

	fmt.Printf("━━━ Sandbox initialized (%s, timeout=%s) ━━━\n\n", dep.Sandbox.Backend, timeout)

	// Root the built-in file tools at the sandbox work directory so YAML tool
	// configs operate on the deployment's repo instead of the CLI's own cwd.
	agents, err := agent.BuildAll(ctx, fc, agent.WithBasePath(dep.Sandbox.WorkDir))
	if err != nil {
		return fmt.Errorf("build agents: %w", err)
	}

	builtAgents := make([]*agent.Agent, 0, len(agents))
	for _, a := range agents {
		registerSandboxTools(a, sb, timeout)
		builtAgents = append(builtAgents, a)
	}

	// Connect any MCP servers declared in YAML and register their tools.
	// Build() records the configs; the connection handshake happens here so
	// deploy/team-run pick up tools like azure-devops without extra wiring.
	for _, a := range builtAgents {
		if err := a.ConnectMCP(ctx); err != nil {
			return fmt.Errorf("connect mcp for agent %q: %w", a.ID, err)
		}
	}
	defer func() {
		for _, a := range builtAgents {
			a.CloseMCP()
		}
	}()

	// Route tool-approval requests to an interactive prompt. Without this,
	// tools declared with permission: require_approval would block silently.
	installInteractiveApprovalHandlers(builtAgents...)

	fmt.Printf("  Built %d agents\n", len(agents))

	if len(fc.Teams) > 0 {
		tc := &fc.Teams[0]
		t, err := assembleTeamFromConfig(tc, agents)
		if err != nil {
			return err
		}

		fmt.Printf("\n━━━ Running team: %s (%s strategy) ━━━\n", tc.Name, tc.Strategy)
		result, err := t.Run(ctx, graph.State{"message": message})
		if err != nil {
			return fmt.Errorf("team run: %w", err)
		}

		if resp, ok := result["response"]; ok {
			fmt.Printf("\n━━━ Result ━━━\n%v\n", resp)
		} else {
			for k, v := range result {
				if strings.HasPrefix(k, "_") {
					continue
				}
				fmt.Printf("  %s: %v\n", k, v)
			}
		}
		fmt.Printf("\n  [%d inter-agent messages exchanged]\n", len(t.MessageHistory()))
	} else if len(fc.Agents) > 0 {
		// No team — run the first agent in declared order.
		firstID := fc.Agents[0].ID
		firstAgent, ok := agents[firstID]
		if !ok {
			return fmt.Errorf("agent %q was not built from config", firstID)
		}
		fmt.Printf("\n━━━ Running agent: %s ━━━\n", firstAgent.Name)
		resp, err := firstAgent.Chat(ctx, message)
		if err != nil {
			return fmt.Errorf("agent chat: %w", err)
		}
		fmt.Printf("\n━━━ Result ━━━\n%s\n", resp.Content)
	}

	fmt.Println("\n✓ Deployment complete.")
	return nil
}

// registerSandboxTools adds sandbox-backed shell and file tools to an agent.
func registerSandboxTools(a *agent.Agent, sb sandbox.Sandbox, timeout time.Duration) {
	sandboxShell := builtins.NewSandboxShellTool(sb, timeout)
	if _, exists := a.Tools.Get("shell"); !exists {
		a.Tools.Register(sandboxShell)
	}
}
