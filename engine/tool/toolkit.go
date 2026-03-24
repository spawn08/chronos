package tool

// Toolkit groups related tool definitions with shared metadata.
type Toolkit struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Permission  Permission    `json:"permission"`
	Tools       []*Definition `json:"tools"`
	Enabled     bool          `json:"enabled"`
}

// NewToolkit creates a new toolkit with the given name.
func NewToolkit(name, description string) *Toolkit {
	return &Toolkit{
		Name:        name,
		Description: description,
		Permission:  PermAllow,
		Enabled:     true,
	}
}

// Add adds a tool definition to the toolkit.
func (tk *Toolkit) Add(def *Definition) *Toolkit {
	if def.Permission == "" {
		def.Permission = tk.Permission
	}
	tk.Tools = append(tk.Tools, def)
	return tk
}

// WithPermission sets the default permission for all tools in the toolkit.
func (tk *Toolkit) WithPermission(p Permission) *Toolkit {
	tk.Permission = p
	return tk
}

// Register registers all tools from this toolkit into the given registry.
// If the toolkit is disabled, no tools are registered.
func (tk *Toolkit) Register(registry *Registry) {
	if !tk.Enabled {
		return
	}
	for _, def := range tk.Tools {
		registry.Register(def)
	}
}

// Enable enables the toolkit.
func (tk *Toolkit) Enable() { tk.Enabled = true }

// Disable disables the toolkit.
func (tk *Toolkit) Disable() { tk.Enabled = false }

// ToolNames returns the names of all tools in the toolkit.
func (tk *Toolkit) ToolNames() []string {
	names := make([]string, len(tk.Tools))
	for i, t := range tk.Tools {
		names[i] = t.Name
	}
	return names
}
