// Package cmd provides the Chronos CLI command tree.
package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spawn08/chronos/cli/repl"
	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/evals"
	chronosos "github.com/spawn08/chronos/os"
	chronostrace "github.com/spawn08/chronos/os/trace"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/team"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/postgres"
	"github.com/spawn08/chronos/storage/adapters/redis"
	"github.com/spawn08/chronos/storage/adapters/sqlite"

	goredis "github.com/redis/go-redis/v9"
)

// Build-time variables set via -ldflags.
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

// Execute runs the root CLI command.
func Execute() error {
	if err := stripGlobalFlags(); err != nil {
		return err
	}
	if len(os.Args) < 2 {
		return printUsage()
	}
	switch os.Args[1] {
	case "repl", "interactive":
		return runREPL()
	case "serve":
		return runServe()
	case "auth":
		return runAuthCmd()
	case "run":
		return runAgent()
	case "pipe":
		return runPipe()
	case "agent", "agents":
		return runAgentCmd()
	case "team", "teams":
		return runTeamCmd()
	case "sessions":
		return runSessions()
	case "memory":
		return runMemory()
	case "db":
		return runDB()
	case "eval", "evals":
		return runEvalCmd()
	case "config":
		return runConfig()
	case "deploy":
		return runDeploy()
	case "monitor":
		return runMonitor()
	case "version":
		return printVersion()
	case "help", "--help", "-h":
		return printUsage()
	default:
		return fmt.Errorf("unknown command: %s\nRun 'chronos help' for usage.", os.Args[1])
	}
}

// stripGlobalFlags extracts process-wide flags before command dispatch. Values
// are exposed through environment variables so every existing subcommand and
// YAML build path observes the same override without duplicating parsing.
func stripGlobalFlags() error {
	kept := os.Args[:1] // preserve the program name
	rest := os.Args[1:]
	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		switch {
		case arg == "-c" || arg == "--config":
			if i+1 >= len(rest) || strings.TrimSpace(rest[i+1]) == "" {
				return fmt.Errorf("global flag %s requires a config path", arg)
			}
			if err := os.Setenv("CHRONOS_CONFIG", rest[i+1]); err != nil {
				return fmt.Errorf("set CHRONOS_CONFIG: %w", err)
			}
			i++ // consume the value token
		case strings.HasPrefix(arg, "--config="):
			value := strings.TrimPrefix(arg, "--config=")
			if value == "" {
				return fmt.Errorf("global flag --config requires a config path")
			}
			if err := os.Setenv("CHRONOS_CONFIG", value); err != nil {
				return fmt.Errorf("set CHRONOS_CONFIG: %w", err)
			}
		case strings.HasPrefix(arg, "-c="):
			value := strings.TrimPrefix(arg, "-c=")
			if value == "" {
				return fmt.Errorf("global flag -c requires a config path")
			}
			if err := os.Setenv("CHRONOS_CONFIG", value); err != nil {
				return fmt.Errorf("set CHRONOS_CONFIG: %w", err)
			}
		case arg == "--permission-mode":
			if i+1 >= len(rest) {
				return fmt.Errorf("global flag --permission-mode requires a value")
			}
			value := rest[i+1]
			if _, err := tool.ParsePermissionMode(value); err != nil {
				return fmt.Errorf("global flag --permission-mode: %w", err)
			}
			if err := os.Setenv("CHRONOS_PERMISSION_MODE", value); err != nil {
				return fmt.Errorf("set CHRONOS_PERMISSION_MODE: %w", err)
			}
			i++
		case strings.HasPrefix(arg, "--permission-mode="):
			value := strings.TrimPrefix(arg, "--permission-mode=")
			if _, err := tool.ParsePermissionMode(value); err != nil {
				return fmt.Errorf("global flag --permission-mode: %w", err)
			}
			if err := os.Setenv("CHRONOS_PERMISSION_MODE", value); err != nil {
				return fmt.Errorf("set CHRONOS_PERMISSION_MODE: %w", err)
			}
		case arg == "--dangerously-skip-permissions":
			if err := os.Setenv("CHRONOS_PERMISSION_MODE", string(tool.PermissionModeAutoApprove)); err != nil {
				return fmt.Errorf("set CHRONOS_PERMISSION_MODE: %w", err)
			}
		case arg == "--debug" || arg == "--no-debug":
			value := strconv.FormatBool(arg == "--debug")
			if err := os.Setenv("CHRONOS_DEBUG", value); err != nil {
				return fmt.Errorf("set CHRONOS_DEBUG: %w", err)
			}
		case arg == "--trace" || arg == "--no-trace":
			value := strconv.FormatBool(arg == "--trace")
			if err := os.Setenv("CHRONOS_TRACE", value); err != nil {
				return fmt.Errorf("set CHRONOS_TRACE: %w", err)
			}
		default:
			kept = append(kept, arg)
		}
	}
	os.Args = kept
	return nil
}

// stripConfigFlag is retained for compatibility with focused parser tests.
func stripConfigFlag() { _ = stripGlobalFlags() }

func printUsage() error {
	fmt.Println(`Chronos CLI — Agentic Framework

Usage:
  chronos [-c <file.yaml>] <command> [subcommand] [options]

Global options:
  -c, --config <file>       Path to the agents YAML config (same as CHRONOS_CONFIG)
  --permission-mode <mode>  prompt (default), auto_approve, or deny
  --dangerously-skip-permissions
                            Auto-approve approval-gated tools for this CLI process
  --debug, --no-debug       Enable or disable agent execution logs on stderr
  --trace, --no-trace       Enable or disable persisted model/tool/graph spans

Commands:
  repl                      Start interactive REPL (loads agent from YAML config)
  serve [addr]              Start ChronosOS control plane server (default :8420)
  auth token [--role <r>] [--tenant <id>] [--ttl <dur>]
                            Mint a dev credential matching CHRONOS_AUTH (apikey or jwt)
  run [--agent <id>] [--stream|--no-stream] <msg>  Run an agent in headless mode
  pipe                      Non-interactive mode: reads from stdin, writes to stdout
  agent list                List agents defined in config
  agent show <id>           Show agent configuration details
  agent chat <id>           Start a chat session with a specific agent
  team list                 List teams defined in config
  team run [--stream|--no-stream] <id> <message>
                            Run a multi-agent team on a task
  team show <id>            Show team configuration details
  deploy <config.yaml> <msg> Deploy agents/team from YAML and run in sandbox
  sessions                  Session management (list, resume, export)
  memory                    Memory management (list, forget, clear)
  db                        Database operations (init, status)
  eval list                 List available eval suites
  eval run <suite.yaml>     Run evaluation suite
  monitor                   Live terminal dashboard (sessions, metrics, latency)
  config                    Configuration (show, validate)
  version                   Print version
  help                      Show this help

Agent Configuration:
  The config file is resolved in this order:
    1. -c/--config <file>  or  CHRONOS_CONFIG=<file>
    2. ./.chronos/agents.yaml  (project-level)
    3. ~/.chronos/agents.yaml  (global)
  Note: in "team show pipeline", 'pipeline' is a team ID, not a filename — point
  the CLI at your file with -c. Example: chronos -c content-pipeline.yaml team show pipeline

Environment:
  CHRONOS_CONFIG          Path to agents YAML config file
  CHRONOS_API_KEY         Default API key for model providers
  CHRONOS_PERMISSION_MODE prompt | auto_approve | deny
  CHRONOS_DEBUG           true to enable debug logs
  CHRONOS_TRACE           true to persist execution spans

Storage (default: SQLite — fully backward compatible):
  CHRONOS_STORAGE_BACKEND  sqlite (default) | postgres | redis
  CHRONOS_DB_PATH          SQLite database path (default: chronos.db) [sqlite]
  CHRONOS_STORAGE_DSN      Postgres DSN [postgres]; redis URL fallback [redis]
  CHRONOS_REDIS_URL        redis://[user:pass@]host:port/db [redis backend]

Cross-replica scheduling & rate limiting (serve):
  CHRONOS_SHARED_STATE  For SQL backends, use a store-backed scheduler (cron
                        fires exactly once across replicas) and a shared SQL
                        rate limiter (cluster-wide limits). Enabled by default
                        for postgres; set true to opt SQLite in, false to opt
                        postgres out. Redis has no SQL-backed shared limiter,
                        so it keeps per-replica in-process limits.

Serve + YAML agents:
  chronos -c agents.yaml serve :8420   Load agents.yaml; any agent with
                                        durable: true (requires a graph:
                                        block) is registered with the
                                        dashboard and /api/dashboard/runs so
                                        its sessions are visible/resumable
                                        from ChronosOS. No config found is
                                        not an error — serve still starts.

Serve auth (opt-in; default is no auth):
  CHRONOS_AUTH          none | jwt | apikey (default: none)
  CHRONOS_JWT_SECRET    HS256 shared secret (jwt mode)
  CHRONOS_JWT_ISSUER    Enforced "iss" claim (jwt mode, optional)
  CHRONOS_JWT_AUDIENCE  Enforced "aud" claim (jwt mode, optional)
  CHRONOS_JWT_JWKS_URL  OIDC/JWKS endpoint for RS256 (jwt mode)
  CHRONOS_API_KEYS      Comma list of "key:role:tenant" (apikey mode; key must not contain ':' or ',')
  CHRONOS_CORS_ORIGINS  Comma list of allowed CORS origins
  CHRONOS_RBAC          true = enforce role checks on /api/* (GET=viewer, writes=user; needs auth)
  CHRONOS_SWAGGER       false = disable the /swagger UI + OpenAPI spec (default: enabled)
  Swagger UI is served at /swagger/ (reachable without auth) unless disabled.`)
	return nil
}

func printVersion() error {
	fmt.Printf("chronos %s\n", Version)
	fmt.Printf("  commit:    %s\n", Commit)
	fmt.Printf("  built:     %s\n", BuildDate)
	fmt.Printf("  go:        %s\n", runtime.Version())
	fmt.Printf("  os/arch:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
	return nil
}

// Storage-selection environment variables. openStore reads them via the
// injected map in resolveStorageConfig so backend selection stays pure and
// unit-testable without opening a real database.
const (
	envStorageBackend = "CHRONOS_STORAGE_BACKEND" // sqlite (default) | postgres | redis
	envDBPath         = "CHRONOS_DB_PATH"         // SQLite file path (sqlite backend)
	envStorageDSN     = "CHRONOS_STORAGE_DSN"     // Postgres DSN (postgres); redis URL fallback
	envRedisURL       = "CHRONOS_REDIS_URL"       // redis://user:pass@host:port/db (redis backend)
)

// storageBackend enumerates the supported storage backends.
type storageBackend string

const (
	backendSQLite   storageBackend = "sqlite"
	backendPostgres storageBackend = "postgres"
	backendRedis    storageBackend = "redis"
)

// storageConfig is the resolved, backend-specific configuration produced from
// the environment by resolveStorageConfig. Only the fields relevant to the
// selected backend are populated.
type storageConfig struct {
	backend storageBackend
	// sqlite
	sqlitePath string
	// postgres
	dsn string
	// redis
	redisAddr     string
	redisPassword string
	redisDB       int
}

// resolveStorageConfig maps the storage environment variables to a concrete
// backend choice and its connection parameters. It is a pure function of the
// supplied env map (redis URL parsing via go-redis is itself pure, doing no
// network I/O), so backend selection can be unit tested without a real
// database. Unknown backends and a missing/invalid config yield a wrapped
// error. The default (unset CHRONOS_STORAGE_BACKEND) is SQLite, preserving
// backward-compatible behavior.
func resolveStorageConfig(env map[string]string) (storageConfig, error) {
	backend := storageBackend(strings.ToLower(strings.TrimSpace(env[envStorageBackend])))
	switch backend {
	case "", backendSQLite:
		path := strings.TrimSpace(env[envDBPath])
		if path == "" {
			path = "chronos.db"
		}
		return storageConfig{backend: backendSQLite, sqlitePath: path}, nil

	case backendPostgres:
		dsn := strings.TrimSpace(env[envStorageDSN])
		if dsn == "" {
			return storageConfig{}, fmt.Errorf("resolve storage config: %s=postgres requires %s to be set", envStorageBackend, envStorageDSN)
		}
		return storageConfig{backend: backendPostgres, dsn: dsn}, nil

	case backendRedis:
		raw := strings.TrimSpace(env[envRedisURL])
		if raw == "" {
			raw = strings.TrimSpace(env[envStorageDSN])
		}
		if raw == "" {
			return storageConfig{}, fmt.Errorf("resolve storage config: %s=redis requires %s (or %s) to be set", envStorageBackend, envRedisURL, envStorageDSN)
		}
		opts, err := goredis.ParseURL(raw)
		if err != nil {
			return storageConfig{}, fmt.Errorf("resolve storage config: parse %s: %w", envRedisURL, err)
		}
		return storageConfig{
			backend:       backendRedis,
			redisAddr:     opts.Addr,
			redisPassword: opts.Password,
			redisDB:       opts.DB,
		}, nil

	default:
		return storageConfig{}, fmt.Errorf("resolve storage config: unknown %s=%q (want sqlite, postgres, or redis)", envStorageBackend, backend)
	}
}

// storageEnv snapshots the storage-selection environment into a map so the pure
// resolveStorageConfig can be unit tested in isolation.
func storageEnv() map[string]string {
	keys := []string{envStorageBackend, envDBPath, envStorageDSN, envRedisURL}
	env := make(map[string]string, len(keys))
	for _, k := range keys {
		env[k] = os.Getenv(k)
	}
	return env
}

// openStore selects a storage backend from the environment, opens it, runs its
// migrations, and returns it as a storage.Storage. The default backend is
// SQLite at CHRONOS_DB_PATH (or chronos.db).
func openStore() (storage.Storage, error) {
	cfg, err := resolveStorageConfig(storageEnv())
	if err != nil {
		return nil, err
	}
	return openStoreFromConfig(cfg)
}

// openStoreFromConfig constructs the concrete adapter for the resolved config,
// migrates it, and returns it as a storage.Storage. On a migration failure the
// store is closed before the error is returned.
func openStoreFromConfig(cfg storageConfig) (storage.Storage, error) {
	var (
		store storage.Storage
		err   error
	)
	switch cfg.backend {
	case backendSQLite:
		store, err = sqlite.New(cfg.sqlitePath)
	case backendPostgres:
		store, err = postgres.New(cfg.dsn)
	case backendRedis:
		store, err = redis.New(cfg.redisAddr, cfg.redisPassword, cfg.redisDB)
	default:
		return nil, fmt.Errorf("open storage: unknown backend %q", cfg.backend)
	}
	if err != nil {
		return nil, fmt.Errorf("open storage (%s): %w", cfg.backend, err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		store.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return store, nil
}

// loadAgentConfig loads agent configuration from YAML,
// falling back to env-based defaults if no config file exists.
func loadAgentConfig() (*agent.FileConfig, error) {
	configPath := os.Getenv("CHRONOS_CONFIG")
	fc, err := agent.LoadFile(configPath)
	if err != nil {
		return nil, err
	}
	return fc, nil
}

// loadAgentByID loads a specific agent from YAML config by ID or name.
func loadAgentByID(idOrName string) (*agent.Agent, error) {
	fc, err := loadAgentConfig()
	if err != nil {
		return nil, err
	}
	cfg, err := fc.FindAgent(idOrName)
	if err != nil {
		return nil, err
	}
	a, err := agent.BuildAgent(context.Background(), cfg)
	if err != nil {
		return nil, err
	}
	if overrideErr := applyCLIRuntimeOverrides(a); overrideErr != nil {
		return nil, overrideErr
	}
	installInteractiveApprovalHandlers(a)
	return a, nil
}

// loadDefaultAgent loads the first agent from YAML config.
func loadDefaultAgent() (*agent.Agent, error) {
	fc, err := loadAgentConfig()
	if err != nil {
		return nil, err
	}
	if len(fc.Agents) == 0 {
		return nil, fmt.Errorf("no agents defined in config")
	}
	a, err := agent.BuildAgent(context.Background(), &fc.Agents[0])
	if err != nil {
		return nil, err
	}
	if overrideErr := applyCLIRuntimeOverrides(a); overrideErr != nil {
		return nil, overrideErr
	}
	installInteractiveApprovalHandlers(a)
	return a, nil
}

func applyCLIRuntimeOverrides(a *agent.Agent) error {
	if a == nil {
		return nil
	}
	if debugValue, configured := os.LookupEnv("CHRONOS_DEBUG"); configured {
		a.Debug = strings.EqualFold(debugValue, "true")
	}
	if traceValue, configured := os.LookupEnv("CHRONOS_TRACE"); configured {
		if strings.EqualFold(traceValue, "true") {
			if a.Storage == nil {
				return fmt.Errorf("CLI tracing for agent %q requires persistent storage; configure storage.backend as sqlite or postgres", a.ID)
			}
			a.Tracer = chronostrace.NewCollector(a.Storage)
		} else {
			a.Tracer = nil
		}
	}
	if modeValue := strings.TrimSpace(os.Getenv("CHRONOS_PERMISSION_MODE")); modeValue != "" && a.Tools != nil {
		mode, err := tool.ParsePermissionMode(modeValue)
		if err != nil {
			return fmt.Errorf("CLI permission mode: %w", err)
		}
		if err := a.Tools.SetPermissionMode(mode); err != nil {
			return fmt.Errorf("CLI permission mode: %w", err)
		}
	}
	return nil
}

func installInteractiveApprovalHandlers(agents ...*agent.Agent) {
	reader := bufio.NewReader(os.Stdin)
	var mu sync.Mutex
	approveAll := false
	handler := func(ctx context.Context, toolName string, args map[string]any) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		if approveAll {
			return true, nil
		}

		fmt.Fprintf(os.Stderr, "\nApproval required for tool %q\nArgs: %s\nApprove? [y/N/a=all for session]: ", toolName, summarizeApprovalArgs(args))
		line, err := reader.ReadString('\n')
		if err != nil && strings.TrimSpace(line) == "" {
			return false, fmt.Errorf("read approval response: %w", err)
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		default:
		}
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer == "a" || answer == "all" {
			approveAll = true
			for _, a := range agents {
				if a != nil && a.Tools != nil {
					_ = a.Tools.SetPermissionMode(tool.PermissionModeAutoApprove)
				}
			}
			fmt.Fprintln(os.Stderr, "Auto-approving approval-gated tools for the rest of this CLI session. Explicitly denied tools remain blocked.")
			return true, nil
		}
		return answer == "y" || answer == "yes", nil
	}
	for _, a := range agents {
		if a != nil && a.Tools != nil {
			a.Tools.SetApprovalHandler(handler)
		}
	}
}

func summarizeApprovalArgs(args map[string]any) string {
	const maxArgLen = 500
	const maxJSONLen = 2000
	redacted := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok && len(s) > maxArgLen {
			redacted[k] = s[:maxArgLen] + fmt.Sprintf("... [truncated, %d bytes total]", len(s))
			continue
		}
		redacted[k] = v
	}
	data, err := json.MarshalIndent(redacted, "", "  ")
	if err != nil {
		return fmt.Sprint(redacted)
	}
	out := string(data)
	if len(out) > maxJSONLen {
		return out[:maxJSONLen] + fmt.Sprintf("... [truncated, %d bytes total]", len(out))
	}
	return out
}

func runREPL() error {
	store, err := openStore()
	if err != nil {
		return err
	}
	defer store.Close()

	r := repl.New(store)

	// Load the full roster (all agents + teams) from YAML config for the REPL.
	if loadErr := attachRoster(r, ""); loadErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load agents from config: %v\n", loadErr)
	}

	return r.Start()
}

// attachRoster loads every agent and team from the YAML config into the REPL so
// /agent can list and switch between them and /team can run teams. When activeID
// is non-empty, that agent (by ID or name) becomes the active chat agent;
// otherwise the first configured agent is active.
func attachRoster(r *repl.REPL, activeID string) error {
	ctx := context.Background()
	fc, err := loadAgentConfig()
	if err != nil {
		return err
	}
	if len(fc.Agents) == 0 {
		return fmt.Errorf("no agents defined in config")
	}

	agents, err := agent.BuildAll(ctx, fc)
	if err != nil {
		return fmt.Errorf("build agents: %w", err)
	}
	builtAgents := make([]*agent.Agent, 0, len(agents))
	for _, a := range agents {
		if overrideErr := applyCLIRuntimeOverrides(a); overrideErr != nil {
			return overrideErr
		}
		builtAgents = append(builtAgents, a)
	}
	installInteractiveApprovalHandlers(builtAgents...)

	// Preserve config order for stable listing.
	list := make([]*agent.Agent, 0, len(fc.Agents))
	for i := range fc.Agents {
		if a, ok := agents[fc.Agents[i].ID]; ok {
			list = append(list, a)
		}
	}
	r.SetAgents(list)

	// Set the requested active agent (resolving ID or name).
	if activeID != "" {
		if cfg, findErr := fc.FindAgent(activeID); findErr == nil {
			if a, ok := agents[cfg.ID]; ok {
				r.SetAgent(a)
			}
		}
	}

	// Wire teams so /team can run them with streaming output.
	if len(fc.Teams) > 0 {
		ids := make([]string, 0, len(fc.Teams))
		for i := range fc.Teams {
			ids = append(ids, fc.Teams[i].ID)
		}
		r.SetTeams(ids, func(ctx context.Context, teamID, message string) (<-chan team.TeamStreamEvent, error) {
			t, buildErr := buildTeamByID(ctx, teamID)
			if buildErr != nil {
				return nil, buildErr
			}
			return t.RunStream(ctx, graph.State{"message": message})
		})
	}
	return nil
}

func runServe() error {
	addr := ":8420"
	if len(os.Args) > 2 {
		addr = os.Args[2]
	}

	// Build auth/CORS options from the environment. The default (no
	// CHRONOS_AUTH) is unchanged: no authentication.
	opts, mode, err := buildServeOptions(serveEnv())
	if err != nil {
		return err
	}

	cfg, err := resolveStorageConfig(storageEnv())
	if err != nil {
		return err
	}

	store, err := openStoreFromConfig(cfg)
	if err != nil {
		return err
	}
	defer store.Close()
	log.Printf("Storage backend: %s", cfg.backend)

	// For SQL-backed, shared storage (Postgres, or SQLite with an explicit
	// opt-in), wire a store-backed scheduler and shared SQL rate limiter so cron
	// fires exactly once and rate limits hold across replicas. Redis and plain
	// SQLite dev usage keep the in-process defaults untouched.
	sharedOpts, sharedClose, err := buildSharedStateOptions(cfg, serveSharedEnv())
	if err != nil {
		return err
	}
	if sharedClose != nil {
		defer func() { _ = sharedClose() }()
	}
	opts = append(opts, sharedOpts...)

	graphOpts, err := buildServeGraphOptions()
	if err != nil {
		return err
	}
	opts = append(opts, graphOpts...)

	srv := chronosos.NewWithOptions(addr, store, opts...)
	log.Printf("Starting ChronosOS on %s (auth: %s)", addr, mode)
	if v, ok := parseBool(os.Getenv(envSwagger)); !ok || v {
		log.Printf("Swagger UI available at http://%s/swagger/", swaggerHost(addr))
	}
	return srv.Start(context.Background())
}

// serveEnv snapshots the serve-related environment variables into a map so the
// pure buildServeOptions builder can be unit tested in isolation.
func serveEnv() map[string]string {
	keys := []string{
		envAuthMode, envJWTSecret, envJWTIssuer, envJWTAudience, envJWTJWKSURL,
		envAPIKeys, envCORSOrigins, envSwagger, envRBAC,
	}
	env := make(map[string]string, len(keys))
	for _, k := range keys {
		env[k] = os.Getenv(k)
	}
	return env
}

// swaggerHost renders a browser-friendly host for the startup log line,
// defaulting a bare ":port" listen address to localhost.
func swaggerHost(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "localhost" + addr
	}
	return addr
}

func runAgent() error {
	// Parse: chronos run [--agent <id>] [--stream|--no-stream] <message...>
	args := os.Args[2:]
	agentID := ""
	streaming := false
	streamSet := false
	var msgParts []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--agent", "-a":
			if i+1 >= len(args) {
				return fmt.Errorf("flag %s requires an agent id\nusage: chronos run [--agent <id>] [--stream] <message>", args[i])
			}
			agentID = args[i+1]
			i++
		case "--stream", "-s":
			streaming = true
			streamSet = true
		case "--no-stream":
			streaming = false
			streamSet = true
		default:
			msgParts = append(msgParts, args[i])
		}
	}

	if len(msgParts) == 0 {
		return fmt.Errorf("usage: chronos run [--agent <id>] [--stream] <message>")
	}
	message := strings.Join(msgParts, " ")

	var a *agent.Agent
	var err error
	if agentID != "" {
		a, err = loadAgentByID(agentID)
	} else {
		a, err = loadDefaultAgent()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load agent from config: %v\n", err)
		fmt.Printf("Message: %s\n", message)
		fmt.Println("Create .chronos/agents.yaml to configure agents. Run 'chronos help' for details.")
		return nil
	}
	if !streamSet && a.StreamConfigured {
		streaming = a.Stream
	}

	// When no agent was explicitly chosen and the config defines more than one,
	// tell the user which one ran and how to target the others.
	if agentID == "" {
		if fc, cfgErr := loadAgentConfig(); cfgErr == nil && len(fc.Agents) > 1 {
			others := make([]string, 0, len(fc.Agents)-1)
			for i := range fc.Agents {
				if fc.Agents[i].ID != a.ID {
					others = append(others, fc.Agents[i].ID)
				}
			}
			fmt.Printf("Using default agent %q. Other agents: %s (select with --agent <id>)\n",
				a.ID, strings.Join(others, ", "))
		}
	}

	fmt.Printf("Agent: %s (model: %s)\n", a.Name, a.Model.Name())
	fmt.Printf("Message: %s\n\n", message)

	if streaming {
		return runAgentStream(a, message)
	}

	resp, err := a.Chat(context.Background(), message)
	if err != nil {
		return fmt.Errorf("chat: %w", err)
	}
	if resp.Reasoning != "" && a.ReasoningConfig.Summary {
		fmt.Fprintf(os.Stderr, "[reasoning summary]\n%s\n[/reasoning summary]\n", resp.Reasoning)
	}
	fmt.Println(resp.Content)
	if resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 {
		fmt.Printf("\n[tokens: %d prompt + %d completion]\n", resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}
	return nil
}

// runAgentStream prints a headless agent response token-by-token via ChatStream.
func runAgentStream(a *agent.Agent, message string) error {
	ch, err := a.ChatStream(context.Background(), message)
	if err != nil {
		return fmt.Errorf("chat: %w", err)
	}
	var usage model.Usage
	reasoningStarted := false
	for chunk := range ch {
		if chunk.Err != nil {
			return fmt.Errorf("chat: %w", chunk.Err)
		}
		if chunk.Delta {
			if chunk.Reasoning != "" {
				if !reasoningStarted {
					fmt.Fprintln(os.Stderr, "[reasoning summary]")
					reasoningStarted = true
				}
				fmt.Fprint(os.Stderr, chunk.Reasoning)
			}
			fmt.Print(chunk.Content)
			continue
		}
		usage = chunk.Usage
	}
	if reasoningStarted {
		fmt.Fprintln(os.Stderr, "\n[/reasoning summary]")
	}
	fmt.Println()
	if usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
		fmt.Printf("\n[tokens: %d prompt + %d completion]\n", usage.PromptTokens, usage.CompletionTokens)
	}
	return nil
}

// --- agent subcommands ---

func runAgentCmd() error {
	sub := "list"
	if len(os.Args) > 2 {
		sub = os.Args[2]
	}
	switch sub {
	case "list":
		return agentList()
	case "show":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: chronos agent show <agent_id>")
		}
		return agentShow(os.Args[3])
	case "chat":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: chronos agent chat <agent_id>")
		}
		return agentChat(os.Args[3])
	default:
		return fmt.Errorf("unknown agent subcommand: %s\nUsage: chronos agent [list|show|chat]", sub)
	}
}

func agentList() error {
	fc, err := loadAgentConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if len(fc.Agents) == 0 {
		fmt.Println("No agents defined.")
		return nil
	}
	fmt.Printf("%-15s %-20s %-15s %-15s %s\n", "ID", "NAME", "PROVIDER", "MODEL", "DESCRIPTION")
	fmt.Println(strings.Repeat("-", 85))
	for i := range fc.Agents {
		desc := fc.Agents[i].Description
		if len(desc) > 30 {
			desc = desc[:27] + "..."
		}
		modelName := fc.Agents[i].Model.Model
		if modelName == "" {
			modelName = "(default)"
		}
		fmt.Printf("%-15s %-20s %-15s %-15s %s\n", fc.Agents[i].ID, fc.Agents[i].Name, fc.Agents[i].Model.Provider, modelName, desc)
	}
	return nil
}

func agentShow(idOrName string) error {
	fc, err := loadAgentConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg, err := fc.FindAgent(idOrName)
	if err != nil {
		return err
	}

	fmt.Printf("Agent: %s\n", cfg.ID)
	fmt.Printf("  Name:          %s\n", cfg.Name)
	if cfg.Description != "" {
		fmt.Printf("  Description:   %s\n", cfg.Description)
	}
	fmt.Printf("  Provider:      %s\n", cfg.Model.Provider)
	modelID := cfg.Model.Model
	if modelID == "" {
		modelID = cfg.Model.Deployment
	}
	fmt.Printf("  Model:         %s\n", modelID)
	if cfg.Model.BaseURL != "" {
		fmt.Printf("  Base URL:      %s\n", cfg.Model.BaseURL)
	}
	fmt.Printf("  Storage:       %s\n", storageLabel(cfg.Storage))
	if cfg.System != "" {
		prompt := cfg.System
		if len(prompt) > 80 {
			prompt = prompt[:77] + "..."
		}
		fmt.Printf("  System Prompt: %s\n", prompt)
	}
	if len(cfg.Instructions) > 0 {
		fmt.Printf("  Instructions:  %d\n", len(cfg.Instructions))
	}
	if len(cfg.Capabilities) > 0 {
		fmt.Printf("  Capabilities:  %s\n", strings.Join(cfg.Capabilities, ", "))
	}
	if len(cfg.SubAgents) > 0 {
		fmt.Printf("  Sub-agents:    %s\n", strings.Join(cfg.SubAgents, ", "))
	}
	if cfg.StreamConfigured {
		fmt.Printf("  Stream:        %t\n", cfg.Stream)
	}
	fmt.Printf("  Debug:         %t\n", cfg.Debug)
	fmt.Printf("  Tracing:       %t\n", cfg.Tracing)
	if cfg.PermissionMode != "" {
		fmt.Printf("  Permissions:   %s\n", cfg.PermissionMode)
	}
	if cfg.Reasoning.Native || cfg.Reasoning.Strategy != "" {
		fmt.Printf("  Reasoning:     strategy=%s native=%t effort=%s budget=%d summary=%t\n",
			cfg.Reasoning.Strategy, cfg.Reasoning.Native, cfg.Reasoning.Effort, cfg.Reasoning.BudgetTokens, cfg.Reasoning.Summary)
	}
	return nil
}

func storageLabel(cfg agent.StorageConfig) string {
	backend := strings.ToLower(strings.TrimSpace(cfg.Backend))
	if backend == "" {
		backend = "sqlite"
	}
	if backend == "sqlite" {
		dsn := cfg.DSN
		if dsn == "" {
			dsn = "chronos.db"
		}
		if dsn != ":memory:" && !filepath.IsAbs(dsn) {
			if absolute, err := filepath.Abs(dsn); err == nil {
				dsn = absolute
			}
		}
		return "sqlite (" + dsn + ")"
	}
	// Never print a PostgreSQL/Redis DSN because it may contain credentials.
	return backend
}

func agentChat(idOrName string) error {
	// Validate the requested agent exists before opening the REPL.
	a, err := loadAgentByID(idOrName)
	if err != nil {
		return err
	}

	store, err := openStore()
	if err != nil {
		return err
	}
	defer store.Close()

	r := repl.New(store)
	// Load the full roster so /agent can switch and /team can run, with the
	// requested agent active. Fall back to the single agent if the roster fails.
	if rosterErr := attachRoster(r, idOrName); rosterErr != nil {
		r.SetAgent(a)
	}
	fmt.Printf("Chatting with agent: %s (%s / %s)\n", a.Name, a.Model.Name(), a.Model.Model())
	return r.Start()
}

// --- team subcommands ---

func runTeamCmd() error {
	sub := "list"
	if len(os.Args) > 2 {
		sub = os.Args[2]
	}
	switch sub {
	case "list":
		return teamList()
	case "show":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: chronos team show <team_id>")
		}
		return teamShow(os.Args[3])
	case "run":
		return teamRun()
	default:
		return fmt.Errorf("unknown team subcommand: %s\nUsage: chronos team [list|show|run]", sub)
	}
}

func teamList() error {
	fc, err := loadAgentConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if len(fc.Teams) == 0 {
		fmt.Println("No teams defined. Add a 'teams:' section to your agents.yaml.")
		return nil
	}
	fmt.Printf("%-15s %-20s %-15s %-10s %s\n", "ID", "NAME", "STRATEGY", "AGENTS", "COORDINATOR")
	fmt.Println(strings.Repeat("-", 80))
	for i := range fc.Teams {
		tc := &fc.Teams[i]
		coord := "-"
		if tc.Coordinator != "" {
			coord = tc.Coordinator
		}
		fmt.Printf("%-15s %-20s %-15s %-10d %s\n", tc.ID, tc.Name, tc.Strategy, len(tc.Agents), coord)
	}
	return nil
}

func teamShow(id string) error {
	fc, err := loadAgentConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	tc, err := fc.FindTeam(id)
	if err != nil {
		return err
	}

	fmt.Printf("Team: %s\n", tc.ID)
	fmt.Printf("  Name:           %s\n", tc.Name)
	fmt.Printf("  Strategy:       %s\n", tc.Strategy)
	fmt.Printf("  Agents:         %s\n", strings.Join(tc.Agents, " → "))
	if tc.Coordinator != "" {
		fmt.Printf("  Coordinator:    %s\n", tc.Coordinator)
	}
	if tc.MaxConcurrency > 0 {
		fmt.Printf("  Max Concurrency: %d\n", tc.MaxConcurrency)
	}
	if tc.MaxIterations > 0 {
		fmt.Printf("  Max Iterations:  %d\n", tc.MaxIterations)
	}
	if tc.ErrorStrategy != "" {
		fmt.Printf("  Error Strategy:  %s\n", tc.ErrorStrategy)
	}
	return nil
}

// buildTeamByID constructs a team from the YAML config by ID, building its member
// agents and applying strategy/coordinator/concurrency settings. Shared by the
// `team run` command and the REPL's /team command.
func buildTeamByID(ctx context.Context, teamID string) (*team.Team, error) {
	fc, err := loadAgentConfig()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	tc, err := fc.FindTeam(teamID)
	if err != nil {
		return nil, err
	}

	agents, err := agent.BuildAll(ctx, fc)
	if err != nil {
		return nil, fmt.Errorf("build agents: %w", err)
	}
	builtAgents := make([]*agent.Agent, 0, len(agents))
	for _, a := range agents {
		if overrideErr := applyCLIRuntimeOverrides(a); overrideErr != nil {
			return nil, overrideErr
		}
		builtAgents = append(builtAgents, a)
	}
	// Connect MCP servers declared in YAML so `team run` picks up their tools.
	// CloseMCP is intentionally not deferred here — the returned Team outlives
	// this function; teamRun handles shutdown when the process exits.
	for _, a := range builtAgents {
		if err := a.ConnectMCP(ctx); err != nil {
			return nil, fmt.Errorf("connect mcp for agent %q: %w", a.ID, err)
		}
	}
	installInteractiveApprovalHandlers(builtAgents...)

	return assembleTeamFromConfig(tc, agents)
}

// assembleTeamFromConfig turns a TeamConfig plus a set of pre-built agents into
// a runnable Team. It handles the strategy-specific wiring (graph compilation
// for swarm/hierarchy, coordinator/router/error-strategy knobs for the plain
// strategies) so callers do not have to duplicate it.
func assembleTeamFromConfig(tc *agent.TeamConfig, agents map[string]*agent.Agent) (*team.Team, error) {
	strategy, err := parseStrategy(tc.Strategy)
	if err != nil {
		return nil, err
	}

	members := make([]*agent.Agent, 0, len(tc.Agents))
	for _, agentID := range tc.Agents {
		a, ok := agents[agentID]
		if !ok {
			return nil, fmt.Errorf("team %q references unknown agent %q", tc.ID, agentID)
		}
		members = append(members, a)
	}

	switch strategy {
	case team.StrategySwarm:
		return buildSwarmTeam(tc, members)
	case team.StrategyHierarchy:
		return buildHierarchyTeam(tc, agents, members)
	}

	t := team.New(tc.ID, tc.Name, strategy)
	for _, a := range members {
		t.AddAgent(a)
	}

	if tc.Coordinator != "" {
		coord, ok := agents[tc.Coordinator]
		if !ok {
			return nil, fmt.Errorf("team %q references unknown coordinator %q", tc.ID, tc.Coordinator)
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
			return nil, esErr
		}
		t.SetErrorStrategy(es)
	}

	if strategy == team.StrategyRouter {
		if err := wireRouter(t, tc, members); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// wireRouter attaches a routing function to a router-strategy team. Without this,
// a router team silently falls back to the capability heuristic, which returns
// the first agent whenever no capability matches the state — so YAML routing
// appears to "always pick agent #1". The default mode is model-based routing.
func wireRouter(t *team.Team, tc *agent.TeamConfig, members []*agent.Agent) error {
	mode := strings.ToLower(strings.TrimSpace(tc.Router))
	if mode == "" {
		mode = "model"
	}
	switch mode {
	case "model":
		provider, err := resolveRouterProvider(tc, members)
		if err != nil {
			return err
		}
		if provider == nil {
			return fmt.Errorf("team %q: router strategy (model) requires a router_model or at least one member agent with a model provider", tc.ID)
		}
		t.SetModelRouter(team.NewModelRouter(provider))
		return nil
	case "capability":
		// Leave both routers nil so selectAgent uses the capability heuristic.
		return nil
	default:
		return fmt.Errorf("team %q: unknown router mode %q (supported: model, capability)", tc.ID, tc.Router)
	}
}

// resolveRouterProvider picks the provider for model-based routing: an explicit
// router_model when configured, otherwise the first member agent's provider.
func resolveRouterProvider(tc *agent.TeamConfig, members []*agent.Agent) (model.Provider, error) {
	if tc.RouterModel.Provider != "" {
		provider, err := agent.BuildProvider(tc.RouterModel)
		if err != nil {
			return nil, fmt.Errorf("team %q: router_model: %w", tc.ID, err)
		}
		return provider, nil
	}
	for _, a := range members {
		if a.Model != nil {
			return a.Model, nil
		}
	}
	return nil, nil
}

// buildSwarmTeam assembles a swarm team (peer-to-peer handoffs) from a flat YAML
// agent list, then restamps it with the configured ID and name.
func buildSwarmTeam(tc *agent.TeamConfig, members []*agent.Agent) (*team.Team, error) {
	t, err := team.NewSwarm(team.SwarmConfig{
		Agents:       members,
		InitialAgent: tc.InitialAgent,
		MaxHandoffs:  tc.MaxHandoffs,
	})
	if err != nil {
		return nil, fmt.Errorf("team %q: %w", tc.ID, err)
	}
	t.ID = tc.ID
	t.Name = tc.Name
	return t, nil
}

// buildHierarchyTeam assembles a two-level hierarchy from a flat YAML list: the
// `coordinator` agent is the root supervisor and every other listed agent is a
// worker under it.
func buildHierarchyTeam(tc *agent.TeamConfig, agents map[string]*agent.Agent, members []*agent.Agent) (*team.Team, error) {
	if tc.Coordinator == "" {
		return nil, fmt.Errorf("team %q: hierarchy strategy requires a 'coordinator' (the root supervisor agent)", tc.ID)
	}
	root, ok := agents[tc.Coordinator]
	if !ok {
		return nil, fmt.Errorf("team %q references unknown coordinator %q", tc.ID, tc.Coordinator)
	}

	workers := make([]*agent.Agent, 0, len(members))
	for _, a := range members {
		if a.ID == tc.Coordinator {
			continue
		}
		workers = append(workers, a)
	}

	t, err := team.NewHierarchy(team.HierarchyConfig{
		Root: &team.SupervisorNode{Supervisor: root, Workers: workers},
	})
	if err != nil {
		return nil, fmt.Errorf("team %q: %w", tc.ID, err)
	}
	t.ID = tc.ID
	t.Name = tc.Name
	return t, nil
}

func teamRun() error {
	// Parse: chronos team run [--stream|--no-stream] <team_id> <message...>
	streaming := false
	streamSet := false
	var positional []string
	for _, arg := range os.Args[3:] {
		switch arg {
		case "--stream", "-s":
			streaming = true
			streamSet = true
		case "--no-stream":
			streaming = false
			streamSet = true
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) < 2 {
		return fmt.Errorf("usage: chronos team run [--stream|--no-stream] <team_id> <message>")
	}
	teamID := positional[0]
	message := strings.Join(positional[1:], " ")

	ctx := context.Background()

	fc, err := loadAgentConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	tc, err := fc.FindTeam(teamID)
	if err != nil {
		return err
	}
	if !streamSet {
		streaming = configuredTeamStreaming(fc, tc)
	}

	t, err := buildTeamByID(ctx, teamID)
	if err != nil {
		return err
	}

	fmt.Printf("Team: %s (%s strategy)\n", tc.Name, tc.Strategy)
	fmt.Printf("Agents: %s\n", strings.Join(tc.Agents, ", "))
	if tc.Coordinator != "" {
		fmt.Printf("Coordinator: %s\n", tc.Coordinator)
	}
	fmt.Printf("Streaming: %t\n", streaming)
	for _, agentID := range tc.Agents {
		cfg, findErr := fc.FindAgent(agentID)
		if findErr != nil {
			continue
		}
		debug, tracing := cfg.Debug, cfg.Tracing
		strategy := normalizedReasoningStrategy(cfg.Reasoning.Strategy)
		native, effort, summary := cfg.Reasoning.Native, cfg.Reasoning.Effort, cfg.Reasoning.Summary
		if built := t.Agents[agentID]; built != nil {
			debug = built.Debug
			tracing = built.Tracer != nil
			strategy = reasoningStrategyLabel(built.Reasoning)
			native = built.ReasoningConfig.Enabled
			effort = built.ReasoningConfig.Effort
			summary = built.ReasoningConfig.Summary
		}
		fmt.Printf("Runtime[%s]: debug=%t tracing=%t reasoning=%s native=%t effort=%s summary=%t\n",
			agentID, debug, tracing, strategy, native, effort, summary)
		if tracing {
			fmt.Printf("Trace store[%s]: %s\n", agentID, storageLabel(cfg.Storage))
		}
	}
	fmt.Printf("Message: %s\n\n", message)

	if streaming {
		return teamRunStream(ctx, t, message)
	}

	result, err := t.Run(ctx, graph.State{"message": message})
	if err != nil {
		return fmt.Errorf("team run: %w", err)
	}

	if resp, ok := result["response"]; ok {
		fmt.Println(resp)
	} else {
		for k, v := range result {
			if strings.HasPrefix(k, "_") {
				continue
			}
			fmt.Printf("%s: %v\n", k, v)
		}
	}

	history := t.MessageHistory()
	if len(history) > 0 {
		fmt.Printf("\n[%d inter-agent messages exchanged]\n", len(history))
	}
	return nil
}

// configuredTeamStreaming applies agent-level YAML streaming preferences to a
// team run. Team execution has a single streaming mode, so it is enabled by
// default only when every participating member (and coordinator, if separate)
// explicitly enables streaming. CLI --stream/--no-stream flags take precedence.
func normalizedReasoningStrategy(strategy string) string {
	if strings.TrimSpace(strategy) == "" {
		return "none"
	}
	return strategy
}

func reasoningStrategyLabel(strategy agent.ReasoningStrategy) string {
	switch strategy {
	case agent.ReasoningCoT:
		return "cot"
	case agent.ReasoningReflection:
		return "reflection"
	default:
		return "none"
	}
}

func configuredTeamStreaming(fc *agent.FileConfig, tc *agent.TeamConfig) bool {
	if fc == nil || tc == nil || len(tc.Agents) == 0 {
		return false
	}

	participantIDs := append([]string(nil), tc.Agents...)
	if tc.Coordinator != "" && !slices.Contains(participantIDs, tc.Coordinator) {
		participantIDs = append(participantIDs, tc.Coordinator)
	}
	for _, id := range participantIDs {
		cfg, err := fc.FindAgent(id)
		if err != nil || !cfg.StreamConfigured || !cfg.Stream {
			return false
		}
	}
	return true
}

// teamRunStream runs a team with token-by-token streaming, printing each agent's
// output under a labeled header. Tokens from different agents may interleave under
// the parallel strategy; the per-agent header marks whose output follows.
func teamRunStream(ctx context.Context, t *team.Team, message string) error {
	ch, err := t.RunStream(ctx, graph.State{"message": message})
	if err != nil {
		return fmt.Errorf("team run: %w", err)
	}

	var current string
	reasoningOpen := make(map[string]bool)
	closeReasoning := func(agentID string) {
		if reasoningOpen[agentID] {
			fmt.Fprintln(os.Stderr, "\n[/reasoning summary]")
			delete(reasoningOpen, agentID)
		}
	}
	for evt := range ch {
		switch evt.Type {
		case team.TeamEventAgentStart:
			fmt.Printf("\n─── %s ───\n", evt.AgentID)
			current = evt.AgentID
		case team.TeamEventReasoning:
			if !reasoningOpen[evt.AgentID] {
				fmt.Fprintf(os.Stderr, "[reasoning summary: %s]\n", evt.AgentID)
				reasoningOpen[evt.AgentID] = true
			}
			fmt.Fprint(os.Stderr, evt.Content)
		case team.TeamEventToken:
			closeReasoning(evt.AgentID)
			// Re-label if a different agent's tokens interleave (parallel strategy).
			if evt.AgentID != current {
				fmt.Printf("\n─── %s ───\n", evt.AgentID)
				current = evt.AgentID
			}
			fmt.Print(evt.Content)
		case team.TeamEventAgentEnd:
			closeReasoning(evt.AgentID)
			fmt.Println()
		case team.TeamEventError:
			return fmt.Errorf("team run: %w", evt.Err)
		case team.TeamEventComplete:
			// Output already streamed; nothing more to print.
		}
	}

	history := t.MessageHistory()
	if len(history) > 0 {
		fmt.Printf("\n[%d inter-agent messages exchanged]\n", len(history))
	}
	return nil
}

func parseStrategy(s string) (team.Strategy, error) {
	switch strings.ToLower(s) {
	case "sequential":
		return team.StrategySequential, nil
	case "parallel":
		return team.StrategyParallel, nil
	case "router":
		return team.StrategyRouter, nil
	case "coordinator":
		return team.StrategyCoordinator, nil
	case "swarm":
		return team.StrategySwarm, nil
	case "hierarchy":
		return team.StrategyHierarchy, nil
	default:
		return "", fmt.Errorf("unknown strategy %q (supported: sequential, parallel, router, coordinator, swarm, hierarchy)", s)
	}
}

func parseErrorStrategy(s string) (team.ErrorStrategy, error) {
	switch strings.ToLower(s) {
	case "fail_fast", "failfast":
		return team.ErrorStrategyFailFast, nil
	case "collect":
		return team.ErrorStrategyCollect, nil
	case "best_effort", "besteffort":
		return team.ErrorStrategyBestEffort, nil
	default:
		return 0, fmt.Errorf("unknown error strategy %q (supported: fail_fast, collect, best_effort)", s)
	}
}

// --- eval subcommands ---

func runEvalCmd() error {
	sub := "list"
	if len(os.Args) > 2 {
		sub = os.Args[2]
	}
	switch sub {
	case "list":
		return evalList()
	case "run":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: chronos eval run <suite.yaml>")
		}
		return evalRun(os.Args[3])
	case "capture":
		return evalCapture(os.Args[3:])
	case "gate":
		return evalGate(os.Args[3:])
	case "history":
		return evalHistory(os.Args[3:])
	default:
		return fmt.Errorf("unknown eval subcommand: %s\nUsage: chronos eval [list|run|capture|gate|history]", sub)
	}
}

// parseEvalFlags parses "--flag value" pairs from args into a map. It fails on an
// unknown flag, a flag missing its value, or a stray positional argument — so a
// mistyped or misplaced flag surfaces as an error instead of silently disabling a
// CI gate check.
func parseEvalFlags(args []string, allowed ...string) (map[string]string, error) {
	allow := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		allow[a] = true
	}
	out := make(map[string]string)
	for i := 0; i < len(args); i++ {
		f := args[i]
		if !allow[f] {
			return nil, fmt.Errorf("unknown or misplaced flag %q", f)
		}
		if i+1 >= len(args) {
			return nil, fmt.Errorf("flag %q needs a value", f)
		}
		out[f] = args[i+1]
		i++
	}
	return out, nil
}

// evalFloatFlag parses a float flag value, erroring on a malformed number so a
// typo cannot silently zero-out (and thereby disable) a gate threshold.
func evalFloatFlag(flags map[string]string, name string, dst *float64) error {
	v, ok := flags[name]
	if !ok {
		return nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fmt.Errorf("flag %s: invalid number %q: %w", name, v, err)
	}
	*dst = f
	return nil
}

// parseGateConfig builds a GateConfig from parsed flags, erroring on a malformed
// threshold value.
func parseGateConfig(flags map[string]string) (evals.GateConfig, error) {
	var cfg evals.GateConfig
	if err := evalFloatFlag(flags, "--min-score", &cfg.MinAvgScore); err != nil {
		return cfg, err
	}
	if err := evalFloatFlag(flags, "--min-pass-rate", &cfg.MinPassRate); err != nil {
		return cfg, err
	}
	if err := evalFloatFlag(flags, "--max-regression", &cfg.MaxRegression); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// evalCapture builds an eval dataset from a stored session's conversation and
// writes it as JSON. Usage:
//
//	chronos evals capture <sessionID> [--name <name>] [--out <file>]
func evalCapture(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: chronos evals capture <sessionID> [--name <name>] [--out <file>]")
	}
	sessionID := args[0]
	flags, err := parseEvalFlags(args[1:], "--name", "--out")
	if err != nil {
		return err
	}
	name, out := sessionID, ""
	if v, ok := flags["--name"]; ok {
		name = v
	}
	out = flags["--out"]

	store, err := openStore()
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	ds, err := evals.CaptureFromSession(context.Background(), store, sessionID, name)
	if err != nil {
		return err
	}
	data, err := evals.MarshalDataset(ds)
	if err != nil {
		return err
	}
	if out == "" {
		fmt.Println(string(data))
		fmt.Fprintf(os.Stderr, "captured %d cases from session %q\n", len(ds.Cases), sessionID)
		return nil
	}
	if err := os.WriteFile(out, data, 0o600); err != nil {
		return fmt.Errorf("write dataset: %w", err)
	}
	fmt.Printf("captured %d cases from session %q → %s\n", len(ds.Cases), sessionID, out)
	return nil
}

// evalGate applies pass/fail thresholds to an eval report and exits non-zero when
// the gate fails, so CI can block regressions. Usage:
//
//	chronos evals gate <report.json> [--baseline <report.json>]
//	    [--min-score <f>] [--min-pass-rate <f>] [--max-regression <f>]
func evalGate(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: chronos evals gate <report.json> [--baseline <f>] [--min-score <f>] [--min-pass-rate <f>] [--max-regression <f>]")
	}
	reportPath := args[0]
	flags, err := parseEvalFlags(args[1:], "--baseline", "--min-score", "--min-pass-rate", "--max-regression")
	if err != nil {
		return err
	}
	baselinePath := flags["--baseline"]
	cfg, err := parseGateConfig(flags)
	if err != nil {
		return err
	}

	report, err := loadReportFile(reportPath)
	if err != nil {
		return err
	}
	var baseline *evals.DatasetReport
	if baselinePath != "" {
		if baseline, err = loadReportFile(baselinePath); err != nil {
			return err
		}
	}

	result := evals.Gate(report, baseline, cfg)
	fmt.Printf("dataset %q: avg_score=%.3f pass_rate=%.3f (%d/%d)\n",
		report.Dataset, report.AvgScore, report.PassRate, report.Passed, report.Total)
	fmt.Println(result.String())
	if !result.Passed {
		return fmt.Errorf("eval gate failed")
	}
	return nil
}

func loadReportFile(path string) (*evals.DatasetReport, error) {
	// #nosec G304 -- path is an operator-supplied CLI argument (the eval report to
	// gate), intentional file access like the adjacent eval-suite/config readers.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read report %s: %w", path, err)
	}
	return evals.LoadReport(data)
}

// evalHistory prints the stored, tenant-scoped run history for a dataset so
// scores are queryable over time. Usage:
//
//	chronos evals history <dataset>
func evalHistory(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: chronos evals history <dataset>")
	}
	dataset := args[0]

	store, err := openStore()
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	hist, err := evals.NewStorageReportStore(store).History(context.Background(), dataset)
	if err != nil {
		return err
	}
	if len(hist) == 0 {
		fmt.Printf("no eval history for dataset %q\n", dataset)
		return nil
	}
	fmt.Printf("eval history for %q (%d runs):\n", dataset, len(hist))
	for _, h := range hist {
		fmt.Printf("  %s  avg_score=%.3f pass_rate=%.3f (%d/%d)\n",
			h.RanAt.Format(time.RFC3339), h.AvgScore, h.PassRate, h.Passed, h.Total)
	}
	return nil
}

func evalList() error {
	patterns := []string{
		".chronos/evals/*.yaml",
		".chronos/evals/*.yml",
		"evals/*.yaml",
		"evals/*.yml",
	}
	found := false
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, m := range matches {
			fmt.Printf("  %s\n", m)
			found = true
		}
	}
	if !found {
		fmt.Println("No eval suites found.")
		fmt.Println("Place eval suite YAML files in .chronos/evals/ or evals/")
	}
	return nil
}

func evalRun(suitePath string) error {
	data, err := os.ReadFile(suitePath)
	if err != nil {
		return fmt.Errorf("read eval suite: %w", err)
	}
	suite, err := evals.LoadSuite(data)
	if err != nil {
		return fmt.Errorf("load eval suite %s: %w", suitePath, err)
	}

	result := suite.Run(context.Background())

	fmt.Printf("Running suite %q (%d cases) from %s\n\n", suite.Name, result.TotalEvals, suitePath)
	for _, r := range result.Results {
		status := "PASS"
		if r.Error != "" {
			status = "ERROR"
		} else if !r.Passed {
			status = "FAIL"
		}
		fmt.Printf("  [%-5s] %-24s score=%.2f  %s\n", status, r.Name, r.Score, r.Details)
		if r.Error != "" {
			fmt.Printf("           error: %s\n", r.Error)
		}
	}
	fmt.Printf("\n%s\n", result.Summary())

	// A non-zero exit lets CI gate on eval outcomes.
	if result.Failed > 0 || result.Errors > 0 {
		return fmt.Errorf("%d/%d evals passed", result.Passed, result.TotalEvals)
	}
	return nil
}

// --- sessions subcommands ---

func runSessions() error {
	sub := "list"
	if len(os.Args) > 2 {
		sub = os.Args[2]
	}
	store, err := openStore()
	if err != nil {
		return err
	}
	defer store.Close()
	ctx := context.Background()

	switch sub {
	case "list":
		agentID := ""
		if len(os.Args) > 3 {
			agentID = os.Args[3]
		}
		return sessionsList(ctx, store, agentID)
	case "resume":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: chronos sessions resume <session_id>")
		}
		return sessionsResume(ctx, store, os.Args[3])
	case "export":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: chronos sessions export <session_id>")
		}
		return sessionsExport(ctx, store, os.Args[3])
	default:
		return fmt.Errorf("unknown sessions subcommand: %s\nUsage: chronos sessions [list|resume|export]", sub)
	}
}

func sessionsList(ctx context.Context, store storage.Storage, agentID string) error {
	sessions, err := store.ListSessions(ctx, agentID, 20, 0)
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	if len(sessions) == 0 {
		fmt.Println("No sessions found.")
		return nil
	}
	fmt.Printf("%-30s %-15s %-12s %s\n", "ID", "AGENT", "STATUS", "CREATED")
	fmt.Println(strings.Repeat("-", 80))
	for _, s := range sessions {
		fmt.Printf("%-30s %-15s %-12s %s\n", s.ID, s.AgentID, s.Status, s.CreatedAt.Format(time.RFC3339))
	}
	return nil
}

func sessionsResume(ctx context.Context, store storage.Storage, sessionID string) error {
	sess, err := store.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("session %q not found: %w", sessionID, err)
	}
	if sess.Status != "running" && sess.Status != "paused" && sess.Status != "active" {
		fmt.Printf("Session %s is in state %q and cannot be resumed.\n", sessionID, sess.Status)
		return nil
	}

	cp, err := store.GetLatestCheckpoint(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("no checkpoint found for session %q: %w", sessionID, err)
	}

	fmt.Printf("Session: %s\n", sess.ID)
	fmt.Printf("Agent:   %s\n", sess.AgentID)
	fmt.Printf("Status:  %s\n", sess.Status)
	fmt.Printf("Checkpoint: node=%s seq=%d\n", cp.NodeID, cp.SeqNum)

	a, loadErr := loadAgentByID(sess.AgentID)
	if loadErr != nil {
		return fmt.Errorf("load agent %q: %w", sess.AgentID, loadErr)
	}

	a.Storage = store

	fmt.Println("\nResuming execution...")
	result, err := a.Resume(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("resume: %w", err)
	}

	fmt.Printf("\nStatus: %s\n", result.Status)
	if result.State != nil {
		stateJSON, _ := json.MarshalIndent(result.State, "", "  ")
		fmt.Printf("State:\n%s\n", string(stateJSON))
	}
	return nil
}

func sessionsExport(ctx context.Context, store storage.Storage, sessionID string) error {
	sess, err := store.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}
	events, err := store.ListEvents(ctx, sessionID, 0)
	if err != nil {
		return fmt.Errorf("list events: %w", err)
	}
	fmt.Printf("# Session %s\n\n", sess.ID)
	fmt.Printf("- Agent: %s\n", sess.AgentID)
	fmt.Printf("- Status: %s\n", sess.Status)
	fmt.Printf("- Created: %s\n\n", sess.CreatedAt.Format(time.RFC3339))
	fmt.Printf("## Events (%d)\n\n", len(events))
	for _, e := range events {
		payload, _ := json.Marshal(e.Payload)
		fmt.Printf("- [seq=%d] %s: %s\n", e.SeqNum, e.Type, string(payload))
	}
	return nil
}

// --- memory subcommands ---

func runMemory() error {
	sub := "list"
	if len(os.Args) > 2 {
		sub = os.Args[2]
	}
	store, err := openStore()
	if err != nil {
		return err
	}
	defer store.Close()
	ctx := context.Background()

	switch sub {
	case "list":
		agentID := ""
		if len(os.Args) > 3 {
			agentID = os.Args[3]
		}
		if agentID == "" {
			return fmt.Errorf("usage: chronos memory list <agent_id>")
		}
		return memoryList(ctx, store, agentID)
	case "forget":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: chronos memory forget <memory_id>")
		}
		return store.DeleteMemory(ctx, os.Args[3])
	case "clear":
		agentID := ""
		if len(os.Args) > 3 {
			agentID = os.Args[3]
		}
		if agentID == "" {
			return fmt.Errorf("usage: chronos memory clear <agent_id>")
		}
		fmt.Printf("Clearing all memories for agent %q\n", agentID)
		mems, err := store.ListMemory(ctx, agentID, "long_term")
		if err != nil {
			return err
		}
		deleted := 0
		var firstErr error
		for _, m := range mems {
			if delErr := store.DeleteMemory(ctx, m.ID); delErr != nil {
				if firstErr == nil {
					firstErr = delErr
				}
				continue
			}
			deleted++
		}
		fmt.Printf("Cleared %d of %d memories.\n", deleted, len(mems))
		if firstErr != nil {
			return fmt.Errorf("some memories could not be deleted: %w", firstErr)
		}
		return nil
	default:
		return fmt.Errorf("unknown memory subcommand: %s\nUsage: chronos memory [list|forget|clear]", sub)
	}
}

func memoryList(ctx context.Context, store storage.Storage, agentID string) error {
	mems, err := store.ListMemory(ctx, agentID, "long_term")
	if err != nil {
		return fmt.Errorf("list memory: %w", err)
	}
	if len(mems) == 0 {
		fmt.Println("No memories found.")
		return nil
	}
	for _, m := range mems {
		fmt.Printf("  [%s] %s = %v\n", m.ID, m.Key, m.Value)
	}
	return nil
}

// --- db subcommands ---

func runDB() error {
	sub := "status"
	if len(os.Args) > 2 {
		sub = os.Args[2]
	}
	switch sub {
	case "init":
		store, err := openStore()
		if err != nil {
			return err
		}
		defer store.Close()
		fmt.Println("Database initialized and migrated successfully.")
		return nil
	case "status":
		dbPath := os.Getenv("CHRONOS_DB_PATH")
		if dbPath == "" {
			dbPath = "chronos.db"
		}
		info, err := os.Stat(dbPath)
		if err != nil {
			fmt.Printf("Database: %s (not found)\n", dbPath)
			return nil
		}
		fmt.Printf("Database: %s\n", dbPath)
		fmt.Printf("Size: %s\n", humanizeBytes(info.Size()))
		fmt.Printf("Modified: %s\n", info.ModTime().Format(time.RFC3339))
		store, err := openStore()
		if err != nil {
			return nil
		}
		defer store.Close()
		sessions, _ := store.ListSessions(context.Background(), "", 1000, 0)
		fmt.Printf("Sessions: %d\n", len(sessions))
		return nil
	default:
		return fmt.Errorf("unknown db subcommand: %s\nUsage: chronos db [init|status]", sub)
	}
}

func humanizeBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// --- config subcommands ---

func runConfig() error {
	sub := "show"
	if len(os.Args) > 2 {
		sub = os.Args[2]
	}
	switch sub {
	case "show":
		fmt.Println("Chronos Configuration:")
		fmt.Printf("  CHRONOS_CONFIG:          %s\n", envOrDefault("CHRONOS_CONFIG", "(auto-detect)"))
		fmt.Printf("  CHRONOS_STORAGE_BACKEND: %s\n", envOrDefault(envStorageBackend, "sqlite"))
		fmt.Printf("  CHRONOS_DB_PATH:         %s\n", envOrDefault("CHRONOS_DB_PATH", "chronos.db"))
		fmt.Printf("  CHRONOS_API_KEY:         %s\n", maskEnv("CHRONOS_API_KEY"))
		fmt.Printf("  CHRONOS_MODEL:           %s\n", envOrDefault("CHRONOS_MODEL", "gpt-4o"))
		fmt.Println()
		// Try to show loaded agents
		fc, err := loadAgentConfig()
		if err == nil && len(fc.Agents) > 0 {
			fmt.Printf("  Agents (%d):\n", len(fc.Agents))
			for j := range fc.Agents {
				fmt.Printf("    - %s (%s / %s)\n", fc.Agents[j].ID, fc.Agents[j].Model.Provider, fc.Agents[j].Model.Model)
			}
		} else {
			fmt.Println("  Agents: none (create .chronos/agents.yaml)")
		}
		return nil
	case "set":
		if len(os.Args) < 5 {
			return fmt.Errorf("usage: chronos config set <key> <value>")
		}
		return configSet(os.Args[3], os.Args[4])
	case "model":
		if len(os.Args) < 4 {
			fmt.Printf("Current model: %s\n", envOrDefault("CHRONOS_MODEL", "gpt-4o"))
			return nil
		}
		return configSet("model", os.Args[3])
	case "validate":
		fc, err := loadAgentConfig()
		if err != nil {
			return fmt.Errorf("config validation failed: %w", err)
		}
		fmt.Printf("Configuration is valid: %d agent(s), %d team(s).\n", len(fc.Agents), len(fc.Teams))
		return nil
	default:
		return fmt.Errorf("unknown config subcommand: %s\nUsage: chronos config [show|validate|set|model]", sub)
	}
}

// configSet persists a key=value pair to ~/.chronos/config.yaml.
func configSet(key, value string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}
	configDir := filepath.Join(home, ".chronos")
	configPath := filepath.Join(configDir, "config.yaml")

	existing := make(map[string]string)
	if data, readErr := os.ReadFile(configPath); readErr == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				existing[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}

	existing[key] = value

	if mkErr := os.MkdirAll(configDir, 0o755); mkErr != nil {
		return fmt.Errorf("create config dir: %w", mkErr)
	}

	var buf strings.Builder
	buf.WriteString("# Chronos CLI configuration\n")
	for k, v := range existing {
		fmt.Fprintf(&buf, "%s: %s\n", k, v)
	}

	if writeErr := os.WriteFile(configPath, []byte(buf.String()), 0o644); writeErr != nil {
		return fmt.Errorf("write config: %w", writeErr)
	}

	fmt.Printf("Set %s = %s (saved to %s)\n", key, value, configPath)
	return nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if v := configLookup(key); v != "" {
		return v
	}
	return def
}

// configLookup reads a value from ~/.chronos/config.yaml by key.
// It maps env-style keys (e.g. "CHRONOS_MODEL") to config-style keys
// (e.g. "model") and does a case-insensitive match.
func configLookup(key string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".chronos", "config.yaml"))
	if err != nil {
		return ""
	}

	normalized := strings.ToLower(strings.TrimPrefix(key, "CHRONOS_"))

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && strings.ToLower(strings.TrimSpace(parts[0])) == normalized {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func maskEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		return "(not set)"
	}
	if len(v) <= 8 {
		return "****"
	}
	return v[:4] + "..." + v[len(v)-4:]
}

func runPipe() error {
	ctx := context.Background()

	var a *agent.Agent
	var err error

	if len(os.Args) > 2 {
		a, err = loadAgentByID(os.Args[2])
	} else {
		a, err = loadDefaultAgent()
	}
	if err != nil {
		return fmt.Errorf("pipe: %w", err)
	}
	// Pipe input and an interactive approval prompt cannot safely share stdin.
	// Fail closed unless YAML/the CLI explicitly selected auto-approve.
	if a.Tools != nil && a.Tools.PermissionMode() == tool.PermissionModePrompt {
		if err := a.Tools.SetPermissionMode(tool.PermissionModeDeny); err != nil {
			return fmt.Errorf("pipe permission mode: %w", err)
		}
	}

	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		resp, err := a.Chat(ctx, line)
		if err != nil {
			_ = encoder.Encode(map[string]any{"error": err.Error()})
			continue
		}

		_ = encoder.Encode(map[string]any{
			"agent":   a.ID,
			"content": resp.Content,
		})
	}
	return scanner.Err()
}
