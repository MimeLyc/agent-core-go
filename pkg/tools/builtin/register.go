package builtin

import "github.com/MimeLyc/agent-core-go/pkg/tools"

// RegisterAll registers all built-in tools with the given registry.
// GitHub API tools are intentionally excluded by default.
func RegisterAll(registry *tools.Registry) {
	RegisterFileTools(registry)
	RegisterSkillTools(registry)
	RegisterBashTools(registry)
	RegisterGitTools(registry)
}

// RegisterAllWithGitHub registers all built-in tools including GitHub API tools.
func RegisterAllWithGitHub(registry *tools.Registry) {
	RegisterAll(registry)
	RegisterGitHubTools(registry)
}

// NewRegistryWithBuiltins creates a new registry with all built-in tools registered.
func NewRegistryWithBuiltins() *tools.Registry {
	registry := tools.NewRegistry()
	RegisterAll(registry)
	return registry
}

// NewRegistryWithBuiltinsAndGitHub creates a registry including GitHub API tools.
func NewRegistryWithBuiltinsAndGitHub() *tools.Registry {
	registry := tools.NewRegistry()
	RegisterAllWithGitHub(registry)
	return registry
}
