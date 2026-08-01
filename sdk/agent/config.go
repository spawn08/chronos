package agent

import (
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
	Instructions []string      `yaml:"instructions,omitempty"`
	Tools        []ToolConfig  `yaml:"tools,omitempty"`
	Capabilities []string      `yaml:"capabilities,omitempty"`

	// MCPServers lists Model Context Protocol servers whose tools are
	// registered with the agent when ConnectMCP is called after Build.
	MCPServers []mcp.ServerConfig `yaml:"mcp_servers,omitempty"`

	OutputSchema   map[string]any `yaml:"output_schema,omitempty"`
	NumHistoryRuns int            `yaml:"num_history_runs,omitempty"`
	Stream         bool           `yaml:"stream,omitempty"`
	Context        ContextYAML    `yaml:"context,omitempty"`

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

// ToolConfig describes a tool to register on the agent.
type ToolConfig struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Parameters  map[string]any `yaml:"parameters,omitempty"` // JSON Schema
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
type FileConfig struct {
	Agents []AgentConfig `yaml:"agents"`
	Teams  []TeamConfig  `yaml:"teams,omitempty"`

	// Defaults applied to all agents unless overridden
	Defaults *AgentConfig `yaml:"defaults,omitempty"`
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
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", resolvedPath, err)
	}

	// Apply defaults to each agent
	if fc.Defaults != nil {
		for i := range fc.Agents {
			applyDefaults(&fc.Agents[i], fc.Defaults)
		}
	}

	// Expand environment variables in all string fields
	for i := range fc.Agents {
		expandEnvInConfig(&fc.Agents[i])
	}
	// Expand env in team-level router model overrides too.
	for i := range fc.Teams {
		expandModelEnv(&fc.Teams[i].RouterModel)
	}

	return &fc, nil
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
	if cfg.System != "" {
		b.WithSystemPrompt(cfg.System)
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
		toolDef, err := buildToolFromConfig(tc, bo.toolHandlers)
		if err != nil {
			return nil, fmt.Errorf("agent %q tool %q: %w", cfg.ID, tc.Name, err)
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
	}

	return b.Build()
}

// BuildAll constructs all agents from a FileConfig. BuildOptions (e.g.
// WithToolHandler) apply to every agent built.
func BuildAll(ctx context.Context, fc *FileConfig, opts ...BuildOption) (map[string]*Agent, error) {
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
func buildToolFromConfig(tc ToolConfig, handlers *toolHandlerRegistry) (*tool.Definition, error) {
	basePath := "."
	switch tc.Name {
	case "shell":
		return builtins.NewShellTool(nil, 0), nil
	case "shell_auto":
		return builtins.NewAutoShellTool(nil, 0), nil
	case "file_read":
		return builtins.NewFileReadTool(basePath), nil
	case "file_write":
		return builtins.NewFileWriteTool(basePath), nil
	case "file_list":
		return builtins.NewFileListTool(basePath), nil
	case "file_glob":
		return builtins.NewFileGlobTool(basePath), nil
	case "file_grep":
		return builtins.NewFileGrepTool(basePath), nil
	default:
		if factory, ok := handlers.lookup(tc.Name); ok {
			handler, err := factory(tc)
			if err != nil {
				return nil, fmt.Errorf("build handler: %w", err)
			}
			if handler == nil {
				return nil, fmt.Errorf("registered factory returned a nil handler")
			}
			return &tool.Definition{
				Name:        tc.Name,
				Description: tc.Description,
				Parameters:  tc.Parameters,
				Permission:  tool.PermAllow,
				Handler:     handler,
			}, nil
		}
		if tc.Description == "" {
			return nil, nil
		}
		name := tc.Name
		return &tool.Definition{
			Name:        tc.Name,
			Description: tc.Description,
			Parameters:  tc.Parameters,
			Permission:  tool.PermAllow,
			Handler: func(_ context.Context, _ map[string]any) (any, error) {
				return nil, fmt.Errorf("tool %q has no registered handler: pass agent.WithToolHandler(%q, ...) to BuildAgent/BuildAll", name, name)
			},
		}, nil
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

func applyDefaults(cfg, defaults *AgentConfig) {
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
