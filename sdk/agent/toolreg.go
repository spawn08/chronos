package agent

import (
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/sdk/skill"
)

// ToolHandlerFactory constructs the runtime handler for a config-declared
// custom tool. It receives the ToolConfig (name, description, JSON-Schema
// parameters) so the factory can honor the declared contract, and returns the
// tool.Handler that executes the tool. A non-nil error aborts agent build.
//
// Bind a YAML tool name to a real handler by passing WithToolHandler to
// BuildAgent/BuildAll, so config-built agents execute working tools instead of
// no-op placeholders.
type ToolHandlerFactory func(tc ToolConfig) (tool.Handler, error)

// toolHandlerRegistry is a build-scoped set of named handler factories. It is
// created per BuildAgent/BuildAll call from the supplied options, so there is no
// process-wide mutable state (matching the instance-scoped tool.Registry /
// skill.Registry pattern used elsewhere in the codebase).
type toolHandlerRegistry struct {
	factories map[string]ToolHandlerFactory
}

func newToolHandlerRegistry() *toolHandlerRegistry {
	return &toolHandlerRegistry{factories: make(map[string]ToolHandlerFactory)}
}

func (r *toolHandlerRegistry) register(name string, factory ToolHandlerFactory) {
	if name == "" || factory == nil {
		return
	}
	r.factories[name] = factory
}

func (r *toolHandlerRegistry) lookup(name string) (ToolHandlerFactory, bool) {
	f, ok := r.factories[name]
	return f, ok
}

// buildOptions carries per-build configuration assembled from BuildOption values.
type buildOptions struct {
	toolHandlers *toolHandlerRegistry
	basePath     string
	// skillCatalog resolves AgentConfig.UseSkills references to skills loaded
	// from FileConfig.SkillsDir. BuildAll populates it automatically; single
	// BuildAgent callers can pass WithSkillCatalog explicitly.
	skillCatalog map[string]*skill.Skill
}

// BuildOption configures BuildAgent/BuildAll. Options are applied per call, so
// nothing leaks between builds.
type BuildOption func(*buildOptions)

// WithToolHandler binds a config tool name to a factory that builds its runtime
// handler for this build. When the agent being built declares a custom tool
// whose name matches, the factory supplies the real handler. An empty name or
// nil factory is ignored. Pass it to BuildAgent/BuildAll:
//
//	agent.BuildAgent(ctx, cfg, agent.WithToolHandler("word_count", factory))
func WithToolHandler(name string, factory ToolHandlerFactory) BuildOption {
	return func(o *buildOptions) { o.toolHandlers.register(name, factory) }
}

// WithBasePath roots the built-in file tools (file_read, file_write, file_list,
// file_glob, file_grep) at the given directory instead of the process working
// directory. An empty path leaves the "." default in place.
func WithBasePath(path string) BuildOption {
	return func(o *buildOptions) { o.basePath = path }
}

// WithSkillCatalog seeds the build with a set of pre-loaded skills that
// AgentConfig.UseSkills entries can reference by name. BuildAll fills this
// automatically from FileConfig.SkillsDir; passing it explicitly is useful
// when driving BuildAgent directly with a hand-built catalog.
func WithSkillCatalog(catalog map[string]*skill.Skill) BuildOption {
	return func(o *buildOptions) { o.skillCatalog = catalog }
}

// newBuildOptions assembles a buildOptions from the given options. The returned
// value always has a non-nil (possibly empty) tool-handler registry.
func newBuildOptions(opts ...BuildOption) *buildOptions {
	o := &buildOptions{toolHandlers: newToolHandlerRegistry()}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	return o
}
