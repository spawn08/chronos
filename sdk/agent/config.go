package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/spawn08/chronos/engine/mcp"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/engine/tool/builtins"
	chronostrace "github.com/spawn08/chronos/os/trace"
	"github.com/spawn08/chronos/sdk/skill"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/postgres"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

// AgentConfig is the YAML-serializable definition of a single agent.
type AgentConfig struct {
	ID          string `yaml:"id"`
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	UserID      string `yaml:"user_id,omitempty"`

	Model        ModelConfig   `yaml:"model"`
	Storage      StorageConfig `yaml:"storage,omitempty"`
	System       string        `yaml:"system_prompt,omitempty"`
	SystemLegacy string        `yaml:"system,omitempty"` // backward-compatible alias
	Instructions []string      `yaml:"instructions,omitempty"`
	Tools        []ToolConfig  `yaml:"tools,omitempty"`
	Capabilities []string      `yaml:"capabilities,omitempty"`

	// MCPServers lists Model Context Protocol servers whose tools are
	// registered with the agent when ConnectMCP is called after Build.
	MCPServers []mcp.ServerConfig `yaml:"mcp_servers,omitempty"`

	// Skills lists reusable capability bundles this agent advertises. Each
	// skill is registered in the agent's skill.Registry, and its Description
	// (plus any Markdown body loaded from ManifestPath) is appended to the
	// system prompt at Build time so the model knows the skill is available.
	Skills []SkillConfig `yaml:"skills,omitempty"`

	// UseSkills references skills by name from the file-level catalog loaded
	// from FileConfig.SkillsDir (SKILL.md files). Referencing an unknown name
	// aborts the build with a clear error.
	UseSkills []string `yaml:"use_skills,omitempty"`

	OutputSchema     map[string]any      `yaml:"output_schema,omitempty"`
	NumHistoryRuns   int                 `yaml:"num_history_runs,omitempty"`
	Stream           bool                `yaml:"stream,omitempty"`
	StreamConfigured bool                `yaml:"-"`
	Debug            bool                `yaml:"debug,omitempty"`
	Tracing          bool                `yaml:"tracing,omitempty"`
	PermissionMode   tool.PermissionMode `yaml:"permission_mode,omitempty"`
	Reasoning        ReasoningYAML       `yaml:"reasoning,omitempty"`
	Context          ContextYAML         `yaml:"context,omitempty"`

	// Team nesting: an agent config can reference sub-agents by ID
	SubAgents []string `yaml:"sub_agents,omitempty"`
}

// ModelConfig describes which model provider and settings to use.
type ModelConfig struct {
	Provider   string `yaml:"provider"`          // openai, anthropic, gemini, mistral, ollama, azure, groq, together, deepseek, openrouter, fireworks, perplexity, anyscale, compatible
	Model      string `yaml:"model,omitempty"`   // model ID, e.g. "gpt-4o", "claude-sonnet-4-6"
	APIKey     string `yaml:"api_key,omitempty"` // literal or ${ENV_VAR}
	BaseURL    string `yaml:"base_url,omitempty"`
	OrgID      string `yaml:"org_id,omitempty"`
	TimeoutSec int    `yaml:"timeout_sec,omitempty"`

	// Azure-specific
	Endpoint   string `yaml:"endpoint,omitempty"`
	Deployment string `yaml:"deployment,omitempty"`
	APIVersion string `yaml:"api_version,omitempty"`
}

// StorageConfig describes the backing store.
type StorageConfig struct {
	Backend string `yaml:"backend,omitempty"` // sqlite, postgres (default: sqlite)
	DSN     string `yaml:"dsn,omitempty"`     // connection string or file path

	// Connection-pool tuning for the SQL backends (sqlite, postgres). A zero
	// value leaves the adapter's built-in default in place.
	MaxOpenConns       int `yaml:"max_open_conns,omitempty"`
	MaxIdleConns       int `yaml:"max_idle_conns,omitempty"`
	ConnMaxLifetimeSec int `yaml:"conn_max_lifetime_sec,omitempty"`
}

// SkillConfig declares a named capability bundle for an agent. Skills are
// descriptive: they advertise what the agent can do (and which tools it uses
// to do it), and their Description + optional Markdown body are injected into
// the system prompt at Build time. Skills do NOT register tools by themselves
// — list those under `tools:` or `mcp_servers:` as usual.
type SkillConfig struct {
	Name         string         `yaml:"name"`
	Version      string         `yaml:"version,omitempty"`
	Description  string         `yaml:"description,omitempty"`
	Author       string         `yaml:"author,omitempty"`
	Tags         []string       `yaml:"tags,omitempty"`
	Tools        []string       `yaml:"tools,omitempty"`
	Manifest     map[string]any `yaml:"manifest,omitempty"`
	ManifestPath string         `yaml:"manifest_path,omitempty"` // Markdown file appended to system prompt
}

// ToolConfig describes a tool to register on the agent.
type ToolConfig struct {
	Name                 string          `yaml:"name"`
	Description          string          `yaml:"description"`
	Parameters           map[string]any  `yaml:"parameters,omitempty"` // JSON Schema
	Permission           tool.Permission `yaml:"permission,omitempty"`
	RequiresConfirmation *bool           `yaml:"requires_confirmation,omitempty"`
	RequiresUserInput    *bool           `yaml:"requires_user_input,omitempty"`
}

// ReasoningYAML configures prompt-based and provider-native reasoning.
type ReasoningYAML struct {
	Strategy     string `yaml:"strategy,omitempty"` // none, cot, reflection
	Native       bool   `yaml:"native,omitempty"`
	Effort       string `yaml:"effort,omitempty"` // low, medium, high
	BudgetTokens int    `yaml:"budget_tokens,omitempty"`
	Summary      bool   `yaml:"summary,omitempty"`
}

// ContextYAML is the YAML-serializable form of ContextConfig.
type ContextYAML struct {
	MaxTokens           int     `yaml:"max_tokens,omitempty"`
	SummarizeThreshold  float64 `yaml:"summarize_threshold,omitempty"`
	PreserveRecentTurns int     `yaml:"preserve_recent_turns,omitempty"`
}

// TeamConfig defines a multi-agent team in YAML.
type TeamConfig struct {
	ID             string   `yaml:"id"`
	Name           string   `yaml:"name"`
	Strategy       string   `yaml:"strategy"`                  // sequential, parallel, router, coordinator, swarm, hierarchy
	Agents         []string `yaml:"agents"`                    // agent IDs (order matters for sequential)
	Coordinator    string   `yaml:"coordinator,omitempty"`     // agent ID: coordinator (coordinator strategy) or root supervisor (hierarchy)
	MaxConcurrency int      `yaml:"max_concurrency,omitempty"` // for parallel strategy
	MaxIterations  int      `yaml:"max_iterations,omitempty"`  // for coordinator strategy
	ErrorStrategy  string   `yaml:"error_strategy,omitempty"`  // fail_fast, collect, best_effort

	// Router selects how the router strategy picks an agent: "model" (default —
	// an LLM reasons over agent descriptions) or "capability" (a zero-LLM
	// heuristic that matches advertised capabilities against state keys).
	Router string `yaml:"router,omitempty"`

	// RouterModel optionally overrides which model drives model-based routing.
	// When its Provider is empty, the router reuses the first member agent's
	// model — set this to route with a cheaper/faster model than the workers.
	RouterModel ModelConfig `yaml:"router_model,omitempty"`

	// InitialAgent is the entry agent for the swarm strategy (defaults to the
	// first listed agent).
	InitialAgent string `yaml:"initial_agent,omitempty"`
	// MaxHandoffs caps peer-to-peer handoffs in the swarm strategy (0 = default).
	MaxHandoffs int `yaml:"max_handoffs,omitempty"`
}

// FileConfig is the top-level structure of a Chronos YAML config file.
// Supports both a single agent and a list of agents, plus optional teams.
//
// The optional Deployment block carries deployment-topology metadata (name,
// sandbox backend, work directory, image, resource caps). It is only consumed
// by `chronos deploy`; the `run`, `team run`, `repl`, and `serve` entry points
// ignore it, so the same YAML can back every command.
type FileConfig struct {
	Agents []AgentConfig `yaml:"agents"`
	Teams  []TeamConfig  `yaml:"teams,omitempty"`

	// Defaults applied to all agents unless overridden
	Defaults *AgentConfig `yaml:"defaults,omitempty"`

	// SkillsDir is a filesystem path (absolute, or relative to the YAML file's
	// directory) that BuildAll walks at load time for SKILL.md files. Every
	// discovered skill forms a catalog keyed by name; agents opt into catalog
	// skills by listing their names under `use_skills:`.
	SkillsDir string `yaml:"skills_dir,omitempty"`

	// Deployment holds `chronos deploy`-only fields. Ignored by other commands.
	Deployment *DeploymentConfig `yaml:"deployment,omitempty"`

	// Deprecated: top-level `name:` — use `deployment.name` instead. Accepted
	// for one release; NormalizeFileConfig promotes it into Deployment and
	// prints a deprecation warning.
	LegacyName string `yaml:"name,omitempty"`
	// Deprecated: top-level `sandbox:` — use `deployment.sandbox` instead.
	LegacySandbox *SandboxConfig `yaml:"sandbox,omitempty"`
}

// DeploymentConfig is the `chronos deploy` metadata block on FileConfig.
type DeploymentConfig struct {
	Name    string        `yaml:"name,omitempty"`
	Sandbox SandboxConfig `yaml:"sandbox,omitempty"`
}

// SandboxConfig configures the isolation boundary a deployment runs inside.
// It maps 1:1 to sandbox.Config; the CLI parses these values into the
// sandbox package's own types.
type SandboxConfig struct {
	Backend string `yaml:"backend,omitempty"` // process, container, k8s
	WorkDir string `yaml:"work_dir,omitempty"`
	Image   string `yaml:"image,omitempty"`
	Network string `yaml:"network,omitempty"`
	Timeout string `yaml:"timeout,omitempty"` // e.g. "5m", "30s"
}

// FindTeam looks up a team by ID (case-insensitive) within a FileConfig.
func (fc *FileConfig) FindTeam(id string) (*TeamConfig, error) {
	for i := range fc.Teams {
		if strings.EqualFold(fc.Teams[i].ID, id) {
			return &fc.Teams[i], nil
		}
	}
	names := make([]string, len(fc.Teams))
	for i := range fc.Teams {
		names[i] = fc.Teams[i].ID
	}
	return nil, fmt.Errorf("team %q not found in config (available: %s)", id, strings.Join(names, ", "))
}

// LoadFile parses a YAML config file and returns all agent configs.
// Searches in order: given path, .chronos/agents.yaml, ~/.chronos/agents.yaml.
func LoadFile(path string) (*FileConfig, error) {
	data, resolvedPath, err := readConfigFile(path)
	if err != nil {
		return nil, err
	}

	var fc FileConfig
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&fc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", resolvedPath, err)
	}
	markExplicitStreamFields(data, &fc)

	NormalizeFileConfig(&fc)
	// Resolve a relative skills_dir against the YAML file's directory so
	// `skills_dir: .chronos/skills` works regardless of the caller's cwd.
	// Absolute paths are left alone.
	if fc.SkillsDir != "" && !filepath.IsAbs(fc.SkillsDir) {
		fc.SkillsDir = filepath.Join(filepath.Dir(resolvedPath), fc.SkillsDir)
	}
	if err := validateFileConfig(&fc); err != nil {
		return nil, fmt.Errorf("validate %s: %w", resolvedPath, err)
	}

	return &fc, nil
}

// markExplicitStreamFields preserves AgentConfig.Stream as a bool for Go API
// compatibility while still distinguishing omitted YAML from `stream: false`.
func markExplicitStreamFields(data []byte, fc *FileConfig) {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil || len(document.Content) == 0 {
		return
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		switch key.Value {
		case "defaults":
			if fc.Defaults != nil && mappingHasKey(value, "stream") {
				fc.Defaults.StreamConfigured = true
			}
		case "agents":
			if value.Kind != yaml.SequenceNode {
				continue
			}
			for agentIndex, agentNode := range value.Content {
				if agentIndex < len(fc.Agents) && mappingHasKey(agentNode, "stream") {
					fc.Agents[agentIndex].StreamConfigured = true
				}
			}
		}
	}
}

func mappingHasKey(node *yaml.Node, name string) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			return true
		}
	}
	return false
}

func validateFileConfig(fc *FileConfig) error {
	agentIDs := make(map[string]struct{}, len(fc.Agents))
	exactAgentIDs := make(map[string]struct{}, len(fc.Agents))
	for i := range fc.Agents {
		cfg := &fc.Agents[i]
		if strings.TrimSpace(cfg.ID) == "" {
			return fmt.Errorf("agents[%d].id is required", i)
		}
		normalizedID := strings.ToLower(strings.TrimSpace(cfg.ID))
		if _, exists := agentIDs[normalizedID]; exists {
			return fmt.Errorf("duplicate agent id %q (agent IDs are case-insensitive)", cfg.ID)
		}
		agentIDs[normalizedID] = struct{}{}
		exactAgentIDs[cfg.ID] = struct{}{}
		if strings.TrimSpace(cfg.Model.Provider) == "" {
			return fmt.Errorf("agent %q model.provider is required", cfg.ID)
		}
		if _, err := tool.ParsePermissionMode(string(cfg.PermissionMode)); err != nil {
			return fmt.Errorf("agent %q permission_mode: %w", cfg.ID, err)
		}
		if _, err := parseReasoningStrategy(cfg.Reasoning.Strategy); err != nil {
			return fmt.Errorf("agent %q reasoning.strategy: %w", cfg.ID, err)
		}
		switch cfg.Reasoning.Effort {
		case "", "low", "medium", "high":
		default:
			return fmt.Errorf("agent %q reasoning.effort %q is invalid (want low, medium, or high)", cfg.ID, cfg.Reasoning.Effort)
		}
		if cfg.Reasoning.BudgetTokens < 0 {
			return fmt.Errorf("agent %q reasoning.budget_tokens must be non-negative", cfg.ID)
		}
		storageBackend := strings.ToLower(strings.TrimSpace(cfg.Storage.Backend))
		if cfg.Tracing && (storageBackend == "none" || storageBackend == "memory") {
			return fmt.Errorf("agent %q tracing requires persistent storage; configure storage.backend as sqlite or postgres", cfg.ID)
		}
		providerName := strings.ToLower(strings.TrimSpace(cfg.Model.Provider))
		if cfg.Reasoning.Native && len(cfg.Tools) > 0 && (providerName == "anthropic" || providerName == "gemini" || providerName == "google") {
			return fmt.Errorf("agent %q: %s native reasoning with tools is not supported because signed thinking blocks cannot be preserved", cfg.ID, providerName)
		}
		for j := range cfg.Tools {
			if strings.TrimSpace(cfg.Tools[j].Name) == "" {
				return fmt.Errorf("agent %q tools[%d].name is required", cfg.ID, j)
			}
			switch cfg.Tools[j].Permission {
			case "", tool.PermAllow, tool.PermRequireApproval, tool.PermDeny:
			default:
				return fmt.Errorf("agent %q tool %q permission %q is invalid", cfg.ID, cfg.Tools[j].Name, cfg.Tools[j].Permission)
			}
		}
		for j := range cfg.MCPServers {
			switch tool.Permission(cfg.MCPServers[j].Permission) {
			case "", tool.PermAllow, tool.PermRequireApproval:
			default:
				return fmt.Errorf("agent %q mcp_servers[%d] (%q) permission %q is invalid (want %q or %q)",
					cfg.ID, j, cfg.MCPServers[j].Name, cfg.MCPServers[j].Permission,
					tool.PermAllow, tool.PermRequireApproval)
			}
		}
		seenSkill := make(map[string]struct{}, len(cfg.Skills))
		for j := range cfg.Skills {
			name := strings.TrimSpace(cfg.Skills[j].Name)
			if name == "" {
				return fmt.Errorf("agent %q skills[%d].name is required", cfg.ID, j)
			}
			if _, dup := seenSkill[name]; dup {
				return fmt.Errorf("agent %q skills: duplicate name %q", cfg.ID, name)
			}
			seenSkill[name] = struct{}{}
		}
	}

	teamIDs := make(map[string]struct{}, len(fc.Teams))
	for i := range fc.Teams {
		teamCfg := &fc.Teams[i]
		if strings.TrimSpace(teamCfg.ID) == "" {
			return fmt.Errorf("teams[%d].id is required", i)
		}
		normalizedID := strings.ToLower(strings.TrimSpace(teamCfg.ID))
		if _, exists := teamIDs[normalizedID]; exists {
			return fmt.Errorf("duplicate team id %q (team IDs are case-insensitive)", teamCfg.ID)
		}
		teamIDs[normalizedID] = struct{}{}
		for _, agentID := range teamCfg.Agents {
			if _, exists := exactAgentIDs[agentID]; !exists {
				return fmt.Errorf("team %q references unknown agent %q (IDs are case-sensitive in team membership)", teamCfg.ID, agentID)
			}
		}
		if teamCfg.Coordinator != "" {
			if _, exists := exactAgentIDs[teamCfg.Coordinator]; !exists {
				return fmt.Errorf("team %q references unknown coordinator %q", teamCfg.ID, teamCfg.Coordinator)
			}
		}
	}
	return nil
}

// FindAgent looks up an agent by ID or name (case-insensitive) within a FileConfig.
func (fc *FileConfig) FindAgent(idOrName string) (*AgentConfig, error) {
	for i := range fc.Agents {
		if strings.EqualFold(fc.Agents[i].ID, idOrName) || strings.EqualFold(fc.Agents[i].Name, idOrName) {
			return &fc.Agents[i], nil
		}
	}
	return nil, fmt.Errorf("agent %q not found in config (available: %s)", idOrName, fc.agentNames())
}

func (fc *FileConfig) agentNames() string {
	names := make([]string, len(fc.Agents))
	for i := range fc.Agents {
		names[i] = fc.Agents[i].ID
		if fc.Agents[i].Name != "" && fc.Agents[i].Name != fc.Agents[i].ID {
			names[i] += " (" + fc.Agents[i].Name + ")"
		}
	}
	return strings.Join(names, ", ")
}

// BuildAgent constructs a fully-wired *Agent from an AgentConfig.
func BuildAgent(ctx context.Context, cfg *AgentConfig, opts ...BuildOption) (*Agent, error) {
	bo := newBuildOptions(opts...)
	b := New(cfg.ID, cfg.Name)
	if cfg.Description != "" {
		b.Description(cfg.Description)
	}
	if cfg.UserID != "" {
		b.WithUserID(cfg.UserID)
	}
	// Compose the system prompt from cfg.System plus any skill blocks so the
	// model sees skill descriptions/manifests inline with its base instructions.
	skillBlock, skillObjs, err := buildSkillsBlock(cfg.Skills, cfg.UseSkills, bo.skillCatalog, bo.basePath)
	if err != nil {
		return nil, fmt.Errorf("agent %q skills: %w", cfg.ID, err)
	}
	for _, s := range skillObjs {
		b.AddSkill(s)
	}
	systemPrompt := cfg.System
	if skillBlock != "" {
		if systemPrompt == "" {
			systemPrompt = skillBlock
		} else {
			systemPrompt = systemPrompt + "\n\n" + skillBlock
		}
	}
	if systemPrompt != "" {
		b.WithSystemPrompt(systemPrompt)
	}
	for _, inst := range cfg.Instructions {
		b.AddInstruction(inst)
	}
	for _, cap := range cfg.Capabilities {
		b.AddCapability(cap)
	}
	if cfg.OutputSchema != nil {
		b.WithOutputSchema(cfg.OutputSchema)
	}
	if cfg.NumHistoryRuns > 0 {
		b.WithHistoryRuns(cfg.NumHistoryRuns)
	}
	if cfg.StreamConfigured || cfg.Stream {
		b.WithStreaming(cfg.Stream)
	}
	b.WithDebug(cfg.Debug)
	reasoningStrategy, reasoningErr := parseReasoningStrategy(cfg.Reasoning.Strategy)
	if reasoningErr != nil {
		return nil, fmt.Errorf("agent %q reasoning: %w", cfg.ID, reasoningErr)
	}
	b.WithReasoning(reasoningStrategy)
	b.WithReasoningConfig(model.ReasoningConfig{
		Enabled:      cfg.Reasoning.Native,
		Effort:       cfg.Reasoning.Effort,
		BudgetTokens: cfg.Reasoning.BudgetTokens,
		Summary:      cfg.Reasoning.Summary,
	})
	permissionMode, permissionErr := tool.ParsePermissionMode(string(cfg.PermissionMode))
	if permissionErr != nil {
		return nil, fmt.Errorf("agent %q permission mode: %w", cfg.ID, permissionErr)
	}
	if setErr := b.agent.Tools.SetPermissionMode(permissionMode); setErr != nil {
		return nil, fmt.Errorf("agent %q permission mode: %w", cfg.ID, setErr)
	}
	if cfg.Context.MaxTokens > 0 || cfg.Context.SummarizeThreshold > 0 || cfg.Context.PreserveRecentTurns > 0 {
		b.WithContextConfig(ContextConfig{
			MaxContextTokens:    cfg.Context.MaxTokens,
			SummarizeThreshold:  cfg.Context.SummarizeThreshold,
			PreserveRecentTurns: cfg.Context.PreserveRecentTurns,
		})
	}

	// Register YAML-defined tools. Built-in names ("shell", "file_read", ...)
	// resolve to their concrete implementations. Custom tool names bind to a
	// handler factory supplied via WithToolHandler; unregistered custom tools
	// fall back to an explicit error placeholder (never a silent no-op).
	for _, tc := range cfg.Tools {
		toolDef, toolErr := buildToolFromConfig(tc, bo.toolHandlers, bo.basePath)
		if toolErr != nil {
			return nil, fmt.Errorf("agent %q tool %q: %w", cfg.ID, tc.Name, toolErr)
		}
		if toolDef != nil {
			b.AddTool(toolDef)
		}
	}

	// MCP servers: register their configs so ConnectMCP can connect and
	// import their tools into the registry after Build.
	for _, srv := range cfg.MCPServers {
		b.AddMCPServer(srv)
	}

	// Model provider
	provider, err := buildProvider(cfg.Model)
	if err != nil {
		return nil, fmt.Errorf("agent %q model: %w", cfg.ID, err)
	}
	b.WithModel(provider)

	// Storage
	store, err := buildStorage(cfg.Storage)
	if err != nil {
		return nil, fmt.Errorf("agent %q storage: %w", cfg.ID, err)
	}
	if store != nil {
		if migrator, ok := store.(interface{ Migrate(context.Context) error }); ok {
			if err := migrator.Migrate(ctx); err != nil {
				return nil, fmt.Errorf("agent %q migrate: %w", cfg.ID, err)
			}
		}
		b.WithStorage(store)
		if cfg.Tracing {
			b.WithTracer(chronostrace.NewCollector(store))
		}
	} else if cfg.Tracing {
		return nil, fmt.Errorf("agent %q tracing requires persistent storage; configure storage backend as sqlite or postgres", cfg.ID)
	}

	return b.Build()
}

// BuildAll constructs all agents from a FileConfig. BuildOptions (e.g.
// WithToolHandler) apply to every agent built.
func BuildAll(ctx context.Context, fc *FileConfig, opts ...BuildOption) (map[string]*Agent, error) {
	// Load the file-level skill catalog (SKILL.md files under skills_dir)
	// once and share it across every agent build. When a caller already
	// supplied WithSkillCatalog, that explicit catalog wins and this pass is
	// a no-op — the option list is walked left-to-right by newBuildOptions.
	if fc.SkillsDir != "" {
		catalog, err := loadSkillCatalog(fc.SkillsDir)
		if err != nil {
			return nil, fmt.Errorf("skills_dir %q: %w", fc.SkillsDir, err)
		}
		opts = append([]BuildOption{WithSkillCatalog(catalog)}, opts...)
	}
	agents := make(map[string]*Agent, len(fc.Agents))
	for i := range fc.Agents {
		a, err := BuildAgent(ctx, &fc.Agents[i], opts...)
		if err != nil {
			return nil, err
		}
		agents[a.ID] = a
	}
	// Wire sub-agents
	for i := range fc.Agents {
		if len(fc.Agents[i].SubAgents) == 0 {
			continue
		}
		parent := agents[fc.Agents[i].ID]
		for _, subID := range fc.Agents[i].SubAgents {
			sub, ok := agents[subID]
			if !ok {
				return nil, fmt.Errorf("agent %q: sub-agent %q not defined", fc.Agents[i].ID, subID)
			}
			parent.SubAgents = append(parent.SubAgents, sub)
		}
	}
	return agents, nil
}

// BuildProvider constructs a model.Provider from a ModelConfig. It is the
// exported entry point used by callers outside this package (e.g. the CLI
// wiring a team's router_model) that need the same provider resolution as
// agent construction.
func BuildProvider(cfg ModelConfig) (model.Provider, error) {
	return buildProvider(cfg)
}

func buildProvider(cfg ModelConfig) (model.Provider, error) {
	apiKey := cfg.APIKey
	modelID := cfg.Model

	switch strings.ToLower(cfg.Provider) {
	case "openai":
		if modelID == "" {
			modelID = "gpt-4o"
		}
		return model.NewOpenAIWithConfig(model.ProviderConfig{
			APIKey: firstNonEmpty(apiKey, os.Getenv("OPENAI_API_KEY")), Model: modelID, BaseURL: cfg.BaseURL,
			OrgID: cfg.OrgID, TimeoutSec: cfg.TimeoutSec,
		}), nil

	case "anthropic":
		if modelID == "" {
			modelID = "claude-sonnet-4-6"
		}
		return model.NewAnthropicWithConfig(model.ProviderConfig{
			APIKey: firstNonEmpty(apiKey, os.Getenv("ANTHROPIC_API_KEY")), Model: modelID, BaseURL: cfg.BaseURL,
			TimeoutSec: cfg.TimeoutSec,
		}), nil

	case "gemini", "google":
		if modelID == "" {
			modelID = "gemini-2.0-flash"
		}
		return model.NewGeminiWithConfig(model.ProviderConfig{
			APIKey: firstNonEmpty(apiKey, os.Getenv("GEMINI_API_KEY"), os.Getenv("GOOGLE_API_KEY")), Model: modelID, BaseURL: cfg.BaseURL,
			TimeoutSec: cfg.TimeoutSec,
		}), nil

	case "mistral":
		if modelID == "" {
			modelID = "mistral-large-latest"
		}
		return model.NewMistralWithConfig(model.ProviderConfig{
			APIKey: firstNonEmpty(apiKey, os.Getenv("MISTRAL_API_KEY")), Model: modelID, BaseURL: cfg.BaseURL,
			TimeoutSec: cfg.TimeoutSec,
		}), nil

	case "ollama":
		host := cfg.BaseURL
		if host == "" {
			host = "http://localhost:11434"
		}
		if modelID == "" {
			modelID = "llama3.3"
		}
		return model.NewOllama(host, modelID), nil

	case "azure":
		endpoint := firstNonEmpty(cfg.Endpoint, cfg.BaseURL, os.Getenv("AZURE_OPENAI_ENDPOINT"))
		deployment := firstNonEmpty(cfg.Deployment, modelID, os.Getenv("AZURE_OPENAI_DEPLOYMENT"))
		apiKey = firstNonEmpty(apiKey, os.Getenv("AZURE_OPENAI_API_KEY"))
		apiVersion := firstNonEmpty(cfg.APIVersion, os.Getenv("AZURE_OPENAI_API_VERSION"))
		if endpoint == "" {
			return nil, fmt.Errorf("azure provider requires endpoint or base_url (or AZURE_OPENAI_ENDPOINT)")
		}
		if deployment == "" {
			return nil, fmt.Errorf("azure provider requires deployment or model (or AZURE_OPENAI_DEPLOYMENT)")
		}
		azCfg := model.AzureConfig{
			ProviderConfig: model.ProviderConfig{
				APIKey:     apiKey,
				BaseURL:    endpoint,
				Model:      deployment,
				TimeoutSec: cfg.TimeoutSec,
			},
			Deployment: deployment,
			APIVersion: apiVersion,
		}
		return model.NewAzureOpenAIWithConfig(azCfg), nil

	case "groq":
		return buildOpenAICompatibleProvider("groq", "https://api.groq.com/openai/v1", cfg, os.Getenv("GROQ_API_KEY")), nil
	case "together":
		return buildOpenAICompatibleProvider("together", "https://api.together.xyz/v1", cfg, os.Getenv("TOGETHER_API_KEY")), nil
	case "deepseek":
		return buildOpenAICompatibleProvider("deepseek", "https://api.deepseek.com/v1", cfg, os.Getenv("DEEPSEEK_API_KEY")), nil
	case "openrouter":
		return buildOpenAICompatibleProvider("openrouter", "https://openrouter.ai/api/v1", cfg, os.Getenv("OPENROUTER_API_KEY")), nil
	case "fireworks":
		return buildOpenAICompatibleProvider("fireworks", "https://api.fireworks.ai/inference/v1", cfg, os.Getenv("FIREWORKS_API_KEY")), nil
	case "perplexity":
		return buildOpenAICompatibleProvider("perplexity", "https://api.perplexity.ai", cfg, os.Getenv("PERPLEXITY_API_KEY")), nil
	case "anyscale":
		return buildOpenAICompatibleProvider("anyscale", "https://api.endpoints.anyscale.com/v1", cfg, os.Getenv("ANYSCALE_API_KEY")), nil
	case "compatible", "custom":
		name := "custom"
		if cfg.Provider == "compatible" {
			name = "compatible"
		}
		baseURL := cfg.BaseURL
		if baseURL == "" {
			return nil, fmt.Errorf("%s provider requires base_url", name)
		}
		return model.NewOpenAICompatibleWithConfig(name, model.ProviderConfig{
			APIKey:     apiKey,
			BaseURL:    baseURL,
			Model:      modelID,
			TimeoutSec: cfg.TimeoutSec,
		}), nil

	default:
		return nil, fmt.Errorf("unknown provider %q (supported: openai, anthropic, gemini, mistral, ollama, azure, groq, together, deepseek, openrouter, fireworks, perplexity, anyscale, compatible)", cfg.Provider)
	}
}

func buildStorage(cfg StorageConfig) (storage.Storage, error) {
	backend := strings.ToLower(cfg.Backend)
	if backend == "" {
		backend = "sqlite"
	}
	switch backend {
	case "sqlite":
		dsn := cfg.DSN
		if dsn == "" {
			dsn = "chronos.db"
		}
		opts := sqlitePoolOptions(cfg)
		store, err := sqlite.New(dsn, opts...)
		if err != nil {
			return nil, fmt.Errorf("sqlite storage: %w", err)
		}
		return store, nil
	case "postgres", "postgresql":
		// The Postgres adapter uses the pgx driver (registered via its blank
		// import) and opens lazily; Migrate is invoked by BuildAgent.
		if cfg.DSN == "" {
			return nil, fmt.Errorf("postgres storage requires dsn")
		}
		store, err := postgres.New(cfg.DSN, postgresPoolOptions(cfg)...)
		if err != nil {
			return nil, fmt.Errorf("postgres storage: %w", err)
		}
		return store, nil
	case "none", "memory":
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown storage backend %q (supported: sqlite, postgres, none)", backend)
	}
}

// sqlitePoolOptions translates StorageConfig pool tuning into sqlite adapter
// options, omitting any left at their zero (adapter-default) value.
func sqlitePoolOptions(cfg StorageConfig) []sqlite.Option {
	var opts []sqlite.Option
	if cfg.MaxOpenConns > 0 {
		opts = append(opts, sqlite.WithMaxOpenConns(cfg.MaxOpenConns))
	}
	if cfg.MaxIdleConns > 0 {
		opts = append(opts, sqlite.WithMaxIdleConns(cfg.MaxIdleConns))
	}
	if cfg.ConnMaxLifetimeSec > 0 {
		opts = append(opts, sqlite.WithConnMaxLifetime(time.Duration(cfg.ConnMaxLifetimeSec)*time.Second))
	}
	return opts
}

// postgresPoolOptions translates StorageConfig pool tuning into postgres
// adapter options, omitting any left at their zero (adapter-default) value.
func postgresPoolOptions(cfg StorageConfig) []postgres.Option {
	var opts []postgres.Option
	if cfg.MaxOpenConns > 0 {
		opts = append(opts, postgres.WithMaxOpenConns(cfg.MaxOpenConns))
	}
	if cfg.MaxIdleConns > 0 {
		opts = append(opts, postgres.WithMaxIdleConns(cfg.MaxIdleConns))
	}
	if cfg.ConnMaxLifetimeSec > 0 {
		opts = append(opts, postgres.WithConnMaxLifetime(time.Duration(cfg.ConnMaxLifetimeSec)*time.Second))
	}
	return opts
}

// buildToolFromConfig resolves a YAML tool config to a tool Definition.
//
// Built-in names (shell, shell_auto, file_read, file_write, file_list,
// file_glob, file_grep) resolve to their concrete implementations. Any other
// name is treated as a custom tool:
//
//   - If a handler factory is registered for the name (via WithToolHandler),
//     the returned handler is bound and the tool is fully functional.
//   - Otherwise, when a description is present, the tool is registered with an
//     explicit placeholder handler that returns an error on invocation — never
//     a silent no-op — so callers learn the tool needs a registered handler.
//   - A custom tool with neither a registered handler nor a description is
//     skipped (returns nil, nil).
func buildToolFromConfig(tc ToolConfig, handlers *toolHandlerRegistry, basePath string) (*tool.Definition, error) {
	if basePath == "" {
		basePath = "."
	}
	var def *tool.Definition

	switch tc.Name {
	case "shell":
		def = builtins.NewShellTool(nil, 0)
	case "shell_auto":
		def = builtins.NewAutoShellTool(nil, 0)
	case "file_read":
		def = builtins.NewFileReadTool(basePath)
	case "file_write":
		def = builtins.NewFileWriteTool(basePath)
	case "file_list":
		def = builtins.NewFileListTool(basePath)
	case "file_glob":
		def = builtins.NewFileGlobTool(basePath)
	case "file_grep":
		def = builtins.NewFileGrepTool(basePath)
	default:
		if factory, ok := handlers.lookup(tc.Name); ok {
			handler, err := factory(tc)
			if err != nil {
				return nil, fmt.Errorf("build handler: %w", err)
			}
			if handler == nil {
				return nil, fmt.Errorf("registered factory returned a nil handler")
			}
			def = &tool.Definition{
				Name:        tc.Name,
				Description: tc.Description,
				Parameters:  tc.Parameters,
				Permission:  tool.PermAllow,
				Handler:     handler,
			}
		} else {
			if tc.Description == "" {
				return nil, nil
			}
			name := tc.Name
			def = &tool.Definition{
				Name:        tc.Name,
				Description: tc.Description,
				Parameters:  tc.Parameters,
				Permission:  tool.PermAllow,
				Handler: func(_ context.Context, _ map[string]any) (any, error) {
					return nil, fmt.Errorf("tool %q has no registered handler: pass agent.WithToolHandler(%q, ...) to BuildAgent/BuildAll", name, name)
				},
			}
		}
	}

	if tc.Permission != "" {
		switch tc.Permission {
		case tool.PermAllow, tool.PermRequireApproval, tool.PermDeny:
			def.Permission = tc.Permission
		default:
			return nil, fmt.Errorf("unknown permission %q (want allow, require_approval, or deny)", tc.Permission)
		}
	}
	if tc.RequiresConfirmation != nil {
		def.RequiresConfirmation = *tc.RequiresConfirmation
	}
	if tc.RequiresUserInput != nil {
		def.RequiresUserInput = *tc.RequiresUserInput
	}
	return def, nil
}

// buildSkillsBlock turns inline SkillConfig entries plus catalog references
// (useRefs) into (a) a prompt fragment injected below cfg.System and (b) the
// *skill.Skill objects to register on the agent.
//
// A manifest_path (if set) is read as UTF-8 text and appended verbatim to the
// skill's block; relative paths resolve against basePath, or CWD when
// basePath is empty. Each useRefs entry is resolved against catalog by name;
// unknown names abort the build so typos fail fast.
func buildSkillsBlock(configs []SkillConfig, useRefs []string, catalog map[string]*skill.Skill, basePath string) (string, []*skill.Skill, error) {
	if len(configs) == 0 && len(useRefs) == 0 {
		return "", nil, nil
	}
	var buf bytes.Buffer
	buf.WriteString("## Available skills\n")
	skills := make([]*skill.Skill, 0, len(configs)+len(useRefs))

	for i := range configs {
		sc := &configs[i]
		name := strings.TrimSpace(sc.Name)
		if name == "" {
			return "", nil, fmt.Errorf("skills[%d].name is required", i)
		}
		s := &skill.Skill{
			Name:        name,
			Version:     sc.Version,
			Description: sc.Description,
			Author:      sc.Author,
			Tags:        append([]string(nil), sc.Tags...),
			Tools:       append([]string(nil), sc.Tools...),
			Manifest:    sc.Manifest,
		}
		if sc.ManifestPath != "" {
			path := sc.ManifestPath
			if !filepath.IsAbs(path) && basePath != "" {
				path = filepath.Join(basePath, path)
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return "", nil, fmt.Errorf("skill %q manifest_path %q: %w", name, sc.ManifestPath, err)
			}
			if s.Manifest == nil {
				s.Manifest = map[string]any{}
			}
			s.Manifest["body"] = string(bytes.TrimSpace(body))
		}
		writeSkillBlock(&buf, s)
		skills = append(skills, s)
	}

	for _, ref := range useRefs {
		name := strings.TrimSpace(ref)
		if name == "" {
			continue
		}
		s, ok := catalog[name]
		if !ok {
			return "", nil, fmt.Errorf("use_skills: %q not found in catalog (loaded from skills_dir)", name)
		}
		writeSkillBlock(&buf, s)
		skills = append(skills, s)
	}
	return strings.TrimRight(buf.String(), "\n"), skills, nil
}

// writeSkillBlock renders one skill into the shared "## Available skills"
// markdown block used to prime the model. Kept in sync between inline and
// catalog-referenced skills so both look identical in the system prompt.
func writeSkillBlock(buf *bytes.Buffer, s *skill.Skill) {
	fmt.Fprintf(buf, "\n### %s", s.Name)
	if s.Version != "" {
		fmt.Fprintf(buf, " (v%s)", s.Version)
	}
	buf.WriteString("\n")
	if s.Description != "" {
		buf.WriteString(s.Description)
		buf.WriteString("\n")
	}
	if len(s.Tools) > 0 {
		fmt.Fprintf(buf, "Tools: %s\n", strings.Join(s.Tools, ", "))
	}
	if s.Manifest != nil {
		if body, ok := s.Manifest["body"].(string); ok && strings.TrimSpace(body) != "" {
			buf.WriteString("\n")
			buf.WriteString(strings.TrimSpace(body))
			buf.WriteString("\n")
		}
	}
}

// loadSkillCatalog walks skillsDir for SKILL.md files and returns a
// name→*Skill map used to resolve AgentConfig.UseSkills references.
func loadSkillCatalog(skillsDir string) (map[string]*skill.Skill, error) {
	skills, err := skill.LoadFromDir(skillsDir)
	if err != nil {
		return nil, err
	}
	catalog := make(map[string]*skill.Skill, len(skills))
	for _, s := range skills {
		if _, dup := catalog[s.Name]; dup {
			return nil, fmt.Errorf("duplicate skill name %q in catalog", s.Name)
		}
		catalog[s.Name] = s
	}
	return catalog, nil
}

func parseReasoningStrategy(value string) (ReasoningStrategy, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "none", "off":
		return ReasoningNone, nil
	case "cot", "chain_of_thought", "chain-of-thought":
		return ReasoningCoT, nil
	case "reflection", "reflect":
		return ReasoningReflection, nil
	default:
		return ReasoningNone, fmt.Errorf("unknown strategy %q (want none, cot, or reflection)", value)
	}
}

func readConfigFile(path string) (data []byte, resolvedPath string, err error) {
	candidates := []string{path}
	if path == "" {
		candidates = []string{
			".chronos/agents.yaml",
			".chronos/agents.yml",
			"agents.yaml",
			"agents.yml",
		}
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates,
				filepath.Join(home, ".chronos", "agents.yaml"),
				filepath.Join(home, ".chronos", "agents.yml"),
			)
		}
	}

	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err == nil {
			return data, p, nil
		}
	}

	if path != "" {
		return nil, path, fmt.Errorf("config file not found: %s", path)
	}
	return nil, "", fmt.Errorf("no agent config found (looked in: %s). "+
		"Pass a file with `-c <file.yaml>` or set CHRONOS_CONFIG=<file.yaml>",
		strings.Join(candidates, ", "))
}

// expandEnvInConfig replaces ${VAR} references with environment variable values.
func expandEnvInConfig(cfg *AgentConfig) {
	cfg.ID = expandEnv(cfg.ID)
	cfg.Name = expandEnv(cfg.Name)
	cfg.Description = expandEnv(cfg.Description)
	cfg.UserID = expandEnv(cfg.UserID)
	cfg.System = expandEnv(cfg.System)
	expandModelEnv(&cfg.Model)
	cfg.Storage.DSN = expandEnv(cfg.Storage.DSN)
	for i := range cfg.Instructions {
		cfg.Instructions[i] = expandEnv(cfg.Instructions[i])
	}
	for i := range cfg.MCPServers {
		cfg.MCPServers[i].Command = expandEnv(cfg.MCPServers[i].Command)
		cfg.MCPServers[i].URL = expandEnv(cfg.MCPServers[i].URL)
		for j := range cfg.MCPServers[i].Args {
			cfg.MCPServers[i].Args[j] = expandEnv(cfg.MCPServers[i].Args[j])
		}
	}
	for i := range cfg.Skills {
		cfg.Skills[i].Description = expandEnv(cfg.Skills[i].Description)
		cfg.Skills[i].ManifestPath = expandEnv(cfg.Skills[i].ManifestPath)
	}
	for i := range cfg.UseSkills {
		cfg.UseSkills[i] = expandEnv(cfg.UseSkills[i])
	}
}

// expandModelEnv replaces ${VAR} references in a ModelConfig's string fields.
func expandModelEnv(m *ModelConfig) {
	m.APIKey = expandEnv(m.APIKey)
	m.Model = expandEnv(m.Model)
	m.BaseURL = expandEnv(m.BaseURL)
	m.Endpoint = expandEnv(m.Endpoint)
	m.Deployment = expandEnv(m.Deployment)
	m.APIVersion = expandEnv(m.APIVersion)
	m.OrgID = expandEnv(m.OrgID)
}

func expandEnv(s string) string {
	if s == "" {
		return s
	}
	return os.ExpandEnv(s)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func buildOpenAICompatibleProvider(name, defaultBaseURL string, cfg ModelConfig, envAPIKey string) model.Provider {
	return model.NewOpenAICompatibleWithConfig(name, model.ProviderConfig{
		APIKey:     firstNonEmpty(cfg.APIKey, envAPIKey),
		BaseURL:    firstNonEmpty(cfg.BaseURL, defaultBaseURL),
		Model:      cfg.Model,
		TimeoutSec: cfg.TimeoutSec,
	})
}

// NormalizeFileConfig applies the standard post-parse pipeline to a FileConfig:
// backward-compatible alias resolution (`system` <- `system_prompt` legacy),
// per-agent default merging via ApplyDefaults, and ${ENV} expansion across
// every string field on every agent and every team's router model. LoadFile
// runs it internally; callers that unmarshal their own YAML wrapper (like the
// deploy CLI, which adds a `sandbox:` block on top of FileConfig) should call
// it before building agents so the two entry points cannot drift apart.
func NormalizeFileConfig(fc *FileConfig) {
	if fc == nil {
		return
	}
	promoteLegacyDeploymentFields(fc)
	if fc.Defaults != nil && fc.Defaults.System == "" {
		fc.Defaults.System = fc.Defaults.SystemLegacy
	}
	for i := range fc.Agents {
		if fc.Agents[i].System == "" {
			fc.Agents[i].System = fc.Agents[i].SystemLegacy
		}
	}
	if fc.Defaults != nil {
		for i := range fc.Agents {
			ApplyDefaults(&fc.Agents[i], fc.Defaults)
		}
	}
	for i := range fc.Agents {
		expandEnvInConfig(&fc.Agents[i])
	}
	for i := range fc.Teams {
		expandModelEnv(&fc.Teams[i].RouterModel)
	}
}

// promoteLegacyDeploymentFields folds pre-existing top-level `name:` and
// `sandbox:` config into the new nested `deployment:` block. Emits a one-time
// deprecation warning to stderr the first time either legacy field is seen in
// a given process, so scripted deploys do not spam. Removal target: one
// release after this change lands.
func promoteLegacyDeploymentFields(fc *FileConfig) {
	if fc.LegacyName == "" && fc.LegacySandbox == nil {
		return
	}
	if fc.Deployment == nil {
		fc.Deployment = &DeploymentConfig{}
	}
	if fc.Deployment.Name == "" && fc.LegacyName != "" {
		fc.Deployment.Name = fc.LegacyName
	}
	if fc.LegacySandbox != nil && (fc.Deployment.Sandbox == SandboxConfig{}) {
		fc.Deployment.Sandbox = *fc.LegacySandbox
	}
	fmt.Fprintln(os.Stderr, "chronos: top-level `name:` and `sandbox:` in config files are deprecated — move them under a nested `deployment:` block. They will be removed in a future release.")
	fc.LegacyName = ""
	fc.LegacySandbox = nil
}

// ApplyDefaults fills in every zero-valued field on cfg from defaults. It is
// used internally by LoadFile and re-exported for callers (like the deploy CLI)
// that unmarshal their own wrapper config and still need the same per-field
// override behavior — provider, endpoint, deployment, api_version, storage,
// system prompt, streaming, reasoning, and so on.
func ApplyDefaults(cfg, defaults *AgentConfig) {
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
	if cfg.Model.Endpoint == "" {
		cfg.Model.Endpoint = defaults.Model.Endpoint
	}
	if cfg.Model.Deployment == "" {
		cfg.Model.Deployment = defaults.Model.Deployment
	}
	if cfg.Model.APIVersion == "" {
		cfg.Model.APIVersion = defaults.Model.APIVersion
	}
	if cfg.Model.OrgID == "" {
		cfg.Model.OrgID = defaults.Model.OrgID
	}
	if cfg.Model.TimeoutSec == 0 {
		cfg.Model.TimeoutSec = defaults.Model.TimeoutSec
	}
	if cfg.Storage.Backend == "" {
		cfg.Storage.Backend = defaults.Storage.Backend
	}
	if cfg.Storage.DSN == "" {
		cfg.Storage.DSN = defaults.Storage.DSN
	}
	if cfg.System == "" {
		cfg.System = defaults.System
	}
	if cfg.NumHistoryRuns == 0 {
		cfg.NumHistoryRuns = defaults.NumHistoryRuns
	}
	if !cfg.StreamConfigured && defaults.StreamConfigured {
		cfg.Stream = defaults.Stream
		cfg.StreamConfigured = true
	}
	if !cfg.Debug {
		cfg.Debug = defaults.Debug
	}
	if !cfg.Tracing {
		cfg.Tracing = defaults.Tracing
	}
	if cfg.PermissionMode == "" {
		cfg.PermissionMode = defaults.PermissionMode
	}
	if cfg.Reasoning.Strategy == "" {
		cfg.Reasoning.Strategy = defaults.Reasoning.Strategy
	}
	if !cfg.Reasoning.Native {
		cfg.Reasoning.Native = defaults.Reasoning.Native
	}
	if cfg.Reasoning.Effort == "" {
		cfg.Reasoning.Effort = defaults.Reasoning.Effort
	}
	if cfg.Reasoning.BudgetTokens == 0 {
		cfg.Reasoning.BudgetTokens = defaults.Reasoning.BudgetTokens
	}
	if !cfg.Reasoning.Summary {
		cfg.Reasoning.Summary = defaults.Reasoning.Summary
	}
	if cfg.Context.MaxTokens == 0 {
		cfg.Context.MaxTokens = defaults.Context.MaxTokens
	}
	if cfg.Context.SummarizeThreshold == 0 {
		cfg.Context.SummarizeThreshold = defaults.Context.SummarizeThreshold
	}
	if cfg.Context.PreserveRecentTurns == 0 {
		cfg.Context.PreserveRecentTurns = defaults.Context.PreserveRecentTurns
	}
}
