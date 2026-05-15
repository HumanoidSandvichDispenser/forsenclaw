package agent

import (
	"sync"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
)

// Agent is the runtime representation of an agent, wrapping its configuration
// with mutable runtime state.
type Agent struct {
	mu sync.RWMutex

	// Definition is the loaded agent configuration. It is replaced on hot-reload.
	Definition *config.AgentDefinition

	// LoadedAt is when this definition was loaded from disk.
	LoadedAt time.Time

	// Active indicates whether the agent is currently loaded and usable.
	Active bool

	// parsedPermissions is a cached copy of the parsed permissions.
	parsedPermissions []config.Permission
}

// NewAgent creates a runtime Agent from a loaded definition.
func NewAgent(def *config.AgentDefinition) (*Agent, error) {
	perms, err := def.ParsedPermissions()
	if err != nil {
		return nil, err
	}
	return &Agent{
		Definition:        def,
		LoadedAt:          time.Now(),
		Active:            true,
		parsedPermissions: perms,
	}, nil
}

// Name returns the agent's name (convenience accessor).
func (a *Agent) Name() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Definition.Name
}

// Permissions returns the parsed permissions (cached).
func (a *Agent) Permissions() []config.Permission {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.parsedPermissions
}

// UpdateDefinition atomically replaces the agent's definition and re-parses permissions.
func (a *Agent) UpdateDefinition(def *config.AgentDefinition) error {
	perms, err := def.ParsedPermissions()
	if err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.Definition = def
	a.LoadedAt = time.Now()
	a.parsedPermissions = perms
	return nil
}

// Deactivate marks the agent as inactive (e.g., on file deletion).
func (a *Agent) Deactivate() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Active = false
}

// IsActive returns whether the agent is currently active.
func (a *Agent) IsActive() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Active
}
