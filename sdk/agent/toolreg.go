package agent

import (
	"sync"

	"github.com/spawn08/chronos/engine/tool"
)

// ToolHandlerFactory constructs the runtime handler for a config-declared
// custom tool. It receives the ToolConfig (name, description, JSON-Schema
// parameters) so the factory can honor the declared contract, and returns the
// tool.Handler that executes the tool. A non-nil error aborts agent build.
//
// Applications bind a YAML tool name to a real handler with RegisterToolHandler
// so that config-built agents (BuildAgent / BuildAll) execute working tools
// instead of no-op placeholders.
type ToolHandlerFactory func(tc ToolConfig) (tool.Handler, error)

// toolHandlerRegistry is a concurrency-safe registry of named handler
// factories keyed by tool name.
type toolHandlerRegistry struct {
	mu        sync.RWMutex
	factories map[string]ToolHandlerFactory
}

func newToolHandlerRegistry() *toolHandlerRegistry {
	return &toolHandlerRegistry{factories: make(map[string]ToolHandlerFactory)}
}

func (r *toolHandlerRegistry) register(name string, factory ToolHandlerFactory) {
	if name == "" || factory == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[name] = factory
}

func (r *toolHandlerRegistry) unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.factories, name)
}

func (r *toolHandlerRegistry) lookup(name string) (ToolHandlerFactory, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.factories[name]
	return f, ok
}

// defaultToolHandlers is the process-wide registry backing the package-level
// RegisterToolHandler API. It follows the standard-library registration idiom
// (cf. database/sql.Register): applications wire named handlers once at startup
// and every config-built agent picks them up.
var defaultToolHandlers = newToolHandlerRegistry()

// RegisterToolHandler binds a config tool name to a factory that builds its
// runtime handler. When an agent is built from YAML/config and declares a
// custom tool whose name matches, the registered factory supplies the real
// handler. Registering with an empty name or nil factory is a no-op.
//
// Registration is safe for concurrent use, but is intended to run once during
// application startup before agents are built.
func RegisterToolHandler(name string, factory ToolHandlerFactory) {
	defaultToolHandlers.register(name, factory)
}

// UnregisterToolHandler removes a previously registered handler factory. It is
// primarily useful in tests to isolate registrations.
func UnregisterToolHandler(name string) {
	defaultToolHandlers.unregister(name)
}

// lookupToolHandler returns the factory registered for name, if any.
func lookupToolHandler(name string) (ToolHandlerFactory, bool) {
	return defaultToolHandlers.lookup(name)
}
