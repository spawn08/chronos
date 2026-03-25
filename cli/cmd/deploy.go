package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/tool/builtins"
	"github.com/spawn08/chronos/sandbox"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/team"
)

// DeployConfig is the YAML config for deploying agents/teams in a sandbox.
type DeployConfig struct {
	Name    string              `yaml:"name"`
	Sandbox DeploySandboxConfig `yaml:"sandbox"`
	Agents  []agent.AgentConfig `yaml:"agents"`
	Teams   []agent.TeamConfig  `yaml:"teams,omitempty"`

	Defaults *agent.AgentConfig `yaml:"defaults,omitempty"`
}

// DeploySandboxConfig defines the sandbox environment for deployment.
type DeploySandboxConfig struct {
	Backend string `yaml:"backend"` // process, container, k8s
	WorkDir string `yaml:"work_dir,omitempty"`
	Image   string `yaml:"image,omitempty"`
	Network string `yaml:"network,omitempty"`
	Timeout string `yaml:"timeout,omitempty"` // e.g. "5m", "30s"
}

func runDeploy() error {
	args := os.Args[2:]
	if len(args) < 2 {
		return fmt.Errorf("usage: chronos deploy <config.yaml> <message>")
	}
	configPath := args[0]
	message := strings.Join(args[1:], " ")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read deploy config: %w", err)
	}

	var dc DeployConfig
	if err = yaml.Unmarshal(data, &dc); err != nil {
		return fmt.Errorf("parse deploy config: %w", err)
	}

	fmt.Println("╔═══════════════════════════════════════════════════╗")
	fmt.Println("║           Chronos Deploy                         ║")
	fmt.Println("╚═══════════════════════════════════════════════════╝")
	fmt.Printf("  Deployment: %s\n", dc.Name)
	fmt.Printf("  Sandbox:    %s\n", dc.Sandbox.Backend)
	fmt.Printf("  Agents:     %d\n", len(dc.Agents))
	fmt.Printf("  Teams:      %d\n", len(dc.Teams))
	fmt.Printf("  Message:    %s\n\n", message)

	ctx := context.Background()

	// Set up sandbox
	timeout := 5 * time.Minute
	if dc.Sandbox.Timeout != "" {
		var td time.Duration
		td, err = time.ParseDuration(dc.Sandbox.Timeout)
		if err == nil {
			timeout = td
		}
	}

	sb, err := sandbox.NewFromConfig(sandbox.Config{
		Backend: sandbox.ParseBackend(dc.Sandbox.Backend),
		WorkDir: dc.Sandbox.WorkDir,
		Image:   dc.Sandbox.Image,
		Network: dc.Sandbox.Network,
	})
	if err != nil {
		return fmt.Errorf("create sandbox: %w", err)
	}
	defer sb.Close()

	fmt.Printf("━━━ Sandbox initialized (%s, timeout=%s) ━━━\n\n", dc.Sandbox.Backend, timeout)

	// Build the FileConfig from the deploy config
	fc := &agent.FileConfig{
		Agents:   dc.Agents,
		Teams:    dc.Teams,
		Defaults: dc.Defaults,
	}

	// Apply defaults
	if fc.Defaults != nil {
		for i := range fc.Agents {
			applyDeployDefaults(&fc.Agents[i], fc.Defaults)
		}
	}

	// Build all agents with sandbox-aware tools
	agents, err := agent.BuildAll(ctx, fc)
	if err != nil {
		return fmt.Errorf("build agents: %w", err)
	}

	// Register sandbox-backed tools on agents that have tool capabilities
	for _, a := range agents {
		registerSandboxTools(a, sb, timeout)
	}

	fmt.Printf("  Built %d agents\n", len(agents))

	// If there are teams, run the first (or specified) team
	if len(dc.Teams) > 0 {
		tc := dc.Teams[0]
		strategy, err := parseStrategy(tc.Strategy)
		if err != nil {
			return err
		}

		t := team.New(tc.ID, tc.Name, strategy)
		for _, agentID := range tc.Agents {
			a, ok := agents[agentID]
			if !ok {
				return fmt.Errorf("team %q references unknown agent %q", tc.ID, agentID)
			}
			t.AddAgent(a)
		}
		if tc.Coordinator != "" {
			coord, ok := agents[tc.Coordinator]
			if !ok {
				return fmt.Errorf("team %q references unknown coordinator %q", tc.ID, tc.Coordinator)
			}
			t.SetCoordinator(coord)
		}
		if tc.MaxConcurrency > 0 {
			t.SetMaxConcurrency(tc.MaxConcurrency)
		}
		if tc.MaxIterations > 0 {
			t.SetMaxIterations(tc.MaxIterations)
		}
		if tc.ErrorStrategy != "" {
			es, esErr := parseErrorStrategy(tc.ErrorStrategy)
			if esErr != nil {
				return esErr
			}
			t.SetErrorStrategy(es)
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
	} else if len(agents) > 0 {
		// No team — run the first agent directly
		var firstAgent *agent.Agent
		for _, a := range agents {
			firstAgent = a
			break
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

func applyDeployDefaults(cfg, defaults *agent.AgentConfig) {
	if cfg.Model.Provider == "" {
		cfg.Model.Provider = defaults.Model.Provider
	}
	if cfg.Model.Model == "" {
		cfg.Model.Model = defaults.Model.Model
	}
	if cfg.Model.APIKey == "" {
		cfg.Model.APIKey = defaults.Model.APIKey
	}
	if cfg.Model.BaseURL == "" {
		cfg.Model.BaseURL = defaults.Model.BaseURL
	}
	if cfg.Storage.Backend == "" {
		cfg.Storage.Backend = defaults.Storage.Backend
	}
	if cfg.System == "" {
		cfg.System = defaults.System
	}
}
