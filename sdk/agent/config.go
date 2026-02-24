package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/storage"
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

// FileConfig is the top-level structure of a Chronos YAML config file.
// Supports both a single agent and a list of agents.
type FileConfig struct {
	Agents []AgentConfig `yaml:"agents"`

	// Defaults applied to all agents unless overridden
	Defaults *AgentConfig `yaml:"defaults,omitempty"`
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

	return &fc, nil
}

// FindAgent looks up an agent by ID or name (case-insensitive) within a FileConfig.
func (fc *FileConfig) FindAgent(idOrName string) (*AgentConfig, error) {
	lower := strings.ToLower(idOrName)
	for i := range fc.Agents {
		if strings.ToLower(fc.Agents[i].ID) == lower || strings.ToLower(fc.Agents[i].Name) == lower {
			return &fc.Agents[i], nil
		}
	}
	return nil, fmt.Errorf("agent %q not found in config (available: %s)", idOrName, fc.agentNames())
}

func (fc *FileConfig) agentNames() string {
	names := make([]string, len(fc.Agents))
	for i, a := range fc.Agents {
		names[i] = a.ID
		if a.Name != "" && a.Name != a.ID {
			names[i] += " (" + a.Name + ")"
		}
	}
	return strings.Join(names, ", ")
}

// BuildAgent constructs a fully-wired *Agent from an AgentConfig.
func BuildAgent(ctx context.Context, cfg *AgentConfig) (*Agent, error) {
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

// BuildAll constructs all agents from a FileConfig.
func BuildAll(ctx context.Context, fc *FileConfig) (map[string]*Agent, error) {
	agents := make(map[string]*Agent, len(fc.Agents))
	for _, cfg := range fc.Agents {
		a, err := BuildAgent(ctx, &cfg)
		if err != nil {
			return nil, err
		}
		agents[a.ID] = a
	}
	// Wire sub-agents
	for _, cfg := range fc.Agents {
		if len(cfg.SubAgents) == 0 {
			continue
		}
		parent := agents[cfg.ID]
		for _, subID := range cfg.SubAgents {
			sub, ok := agents[subID]
			if !ok {
				return nil, fmt.Errorf("agent %q: sub-agent %q not defined", cfg.ID, subID)
			}
			parent.SubAgents = append(parent.SubAgents, sub)
		}
	}
	return agents, nil
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
			APIKey: apiKey, Model: modelID, BaseURL: cfg.BaseURL,
			OrgID: cfg.OrgID, TimeoutSec: cfg.TimeoutSec,
		}), nil

	case "anthropic":
		if modelID == "" {
			modelID = "claude-sonnet-4-6"
		}
		return model.NewAnthropicWithConfig(model.ProviderConfig{
			APIKey: apiKey, Model: modelID, BaseURL: cfg.BaseURL,
			TimeoutSec: cfg.TimeoutSec,
		}), nil

	case "gemini", "google":
		if modelID == "" {
			modelID = "gemini-2.0-flash"
		}
		return model.NewGeminiWithConfig(model.ProviderConfig{
			APIKey: apiKey, Model: modelID, BaseURL: cfg.BaseURL,
			TimeoutSec: cfg.TimeoutSec,
		}), nil

	case "mistral":
		if modelID == "" {
			modelID = "mistral-large-latest"
		}
		return model.NewMistralWithConfig(model.ProviderConfig{
			APIKey: apiKey, Model: modelID, BaseURL: cfg.BaseURL,
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
		azCfg := model.AzureConfig{
			ProviderConfig: model.ProviderConfig{
				APIKey:     apiKey,
				BaseURL:    cfg.Endpoint,
				Model:      cfg.Deployment,
				TimeoutSec: cfg.TimeoutSec,
			},
			Deployment: cfg.Deployment,
			APIVersion: cfg.APIVersion,
		}
		return model.NewAzureOpenAIWithConfig(azCfg), nil

	case "groq":
		return model.NewGroq(apiKey, modelID), nil
	case "together":
		return model.NewTogether(apiKey, modelID), nil
	case "deepseek":
		return model.NewDeepSeek(apiKey, modelID), nil
	case "openrouter":
		return model.NewOpenRouter(apiKey, modelID), nil
	case "fireworks":
		return model.NewFireworks(apiKey, modelID), nil
	case "perplexity":
		return model.NewPerplexity(apiKey, modelID), nil
	case "anyscale":
		return model.NewAnyscale(apiKey, modelID), nil
	case "compatible", "custom":
		name := "custom"
		if cfg.Provider == "compatible" {
			name = "compatible"
		}
		return model.NewOpenAICompatible(name, cfg.BaseURL, apiKey, modelID), nil

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
		return sqlite.New(dsn)
	case "postgres", "postgresql":
		// Postgres adapter is available but requires DSN
		if cfg.DSN == "" {
			return nil, fmt.Errorf("postgres storage requires dsn")
		}
		// Import dynamically avoided — callers should wire manually for postgres.
		// Return nil so callers know to configure it.
		return nil, fmt.Errorf("postgres storage: set up programmatically via storage/adapters/postgres.New(dsn)")
	case "none", "memory":
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown storage backend %q (supported: sqlite, postgres, none)", backend)
	}
}

func readConfigFile(path string) ([]byte, string, error) {
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
	return nil, "", fmt.Errorf("no agent config found (looked in: %s)", strings.Join(candidates, ", "))
}

// expandEnvInConfig replaces ${VAR} references with environment variable values.
func expandEnvInConfig(cfg *AgentConfig) {
	cfg.ID = expandEnv(cfg.ID)
	cfg.Name = expandEnv(cfg.Name)
	cfg.Description = expandEnv(cfg.Description)
	cfg.UserID = expandEnv(cfg.UserID)
	cfg.System = expandEnv(cfg.System)
	cfg.Model.APIKey = expandEnv(cfg.Model.APIKey)
	cfg.Model.BaseURL = expandEnv(cfg.Model.BaseURL)
	cfg.Model.Endpoint = expandEnv(cfg.Model.Endpoint)
	cfg.Model.Deployment = expandEnv(cfg.Model.Deployment)
	cfg.Model.OrgID = expandEnv(cfg.Model.OrgID)
	cfg.Storage.DSN = expandEnv(cfg.Storage.DSN)
	for i := range cfg.Instructions {
		cfg.Instructions[i] = expandEnv(cfg.Instructions[i])
	}
}

func expandEnv(s string) string {
	if s == "" {
		return s
	}
	return os.ExpandEnv(s)
}

func applyDefaults(cfg *AgentConfig, defaults *AgentConfig) {
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
