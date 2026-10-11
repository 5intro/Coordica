package runner

import (
	"fmt"

	"coordica/internal/agent"
)

// LocalRegistry builds drivers only from trusted machine-local configuration.
// Task assignments and center responses cannot introduce executable commands.
func LocalRegistry(tools []agent.Definition) (*agent.Registry, error) {
	definitions := append(agent.BuiltinDefinitions(), tools...)
	if len(definitions) > 64 {
		return nil, fmt.Errorf("local tool registry exceeds 64 definitions")
	}
	return agent.NewRegistry(definitions...)
}

// NormalizeExecutables keeps a single canonical ID per registered driver and
// rejects misspelled or unregistered override keys before any local execution.
func NormalizeExecutables(registry *agent.Registry, overrides map[string]string) (map[string]string, error) {
	result := make(map[string]string, len(overrides))
	for id, path := range overrides {
		definition, ok := registry.Resolve(id)
		if !ok {
			return nil, fmt.Errorf("executable override names unregistered tool %q", id)
		}
		if _, duplicate := result[definition.ID]; duplicate {
			return nil, fmt.Errorf("duplicate executable override for %q", definition.ID)
		}
		result[definition.ID] = path
	}
	return result, nil
}
