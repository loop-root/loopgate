package tools

import (
	"fmt"
	"sort"
)

// Registry holds registered tools and provides lookup.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// TryRegister adds a tool to the registry and returns an error on duplicates.
func (r *Registry) TryRegister(tool Tool) error {
	name := tool.Name()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool already registered: %s", name)
	}
	r.tools[name] = tool
	return nil
}

// Register adds a tool to the registry.
// Returns an error if a tool with the same name is already registered.
func (r *Registry) Register(tool Tool) error {
	return r.TryRegister(tool)
}

// Get returns a tool by name, or nil if not found.
func (r *Registry) Get(name string) Tool {
	return r.tools[name]
}

// Has returns true if a tool with the given name is registered.
func (r *Registry) Has(name string) bool {
	_, ok := r.tools[name]
	return ok
}

// List returns all registered tool names in sorted order.
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// All returns all registered tools.
func (r *Registry) All() []Tool {
	tools := make([]Tool, 0, len(r.tools))
	for _, name := range r.List() {
		tools = append(tools, r.tools[name])
	}
	return tools
}
