package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
)

// agentEntry groups an agent definition with its runtime.
// runtime is nil when registry is not configured (e.g. in tests).
type agentEntry struct {
	agent   *Agent
	runtime *AgentRuntime
}

// ManagerDeps groups optional runtime dependencies for Manager.
// If Registry is nil, no AgentRuntimes are created (useful in tests).
type ManagerDeps struct {
	Registry       *inference.Registry
	Assembler      Assembler
	Executor       ToolExecutor
	Notifier       ConfirmationNotifier
	ResponseWriter ResponseWriter
}

// Manager loads, tracks, and hot-reloads agent definitions from disk.
// It also owns the AgentRuntime for each agent.
type Manager struct {
	mu      sync.RWMutex
	entries map[string]*agentEntry
	watcher *fsnotify.Watcher
	paths   *paths.Paths
	server  *config.ServerConfig

	// Runtime deps — nil Registry means runtimes are not created (e.g. in tests).
	registry             *inference.Registry
	assembler            Assembler
	executor             ToolExecutor
	confirmationRegistry *ConfirmationRegistry
	notifier             ConfirmationNotifier
	responseWriter       ResponseWriter

	ctx    context.Context
	cancel context.CancelFunc
}

// NewManager creates a manager and loads all agents from disk.
func NewManager(p *paths.Paths, serverCfg *config.ServerConfig, deps ManagerDeps) (*Manager, error) {
	ctx, cancel := context.WithCancel(context.Background())
	confirmationRegistry := NewConfirmationRegistry()
	m := &Manager{
		entries:              make(map[string]*agentEntry),
		paths:                p,
		server:               serverCfg,
		registry:             deps.Registry,
		assembler:            deps.Assembler,
		executor:             deps.Executor,
		confirmationRegistry: confirmationRegistry,
		notifier:             deps.Notifier,
		responseWriter:       deps.ResponseWriter,
		ctx:                  ctx,
		cancel:               cancel,
	}

	// Initial load
	if err := m.loadAll(); err != nil {
		return nil, fmt.Errorf("loading agents: %w", err)
	}

	// Set up fsnotify watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("creating fsnotify watcher: %w", err)
	}
	m.watcher = watcher

	// Watch the agents config directory
	agentsDir := p.AgentsConfigDir()
	if err := watcher.Add(agentsDir); err != nil {
		return nil, fmt.Errorf("watching agents dir: %w", err)
	}

	// Recursively watch each agent subdirectory
	entries, err := os.ReadDir(agentsDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading agents dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			agentDir := filepath.Join(agentsDir, entry.Name())
			if err := watcher.Add(agentDir); err != nil {
				log.Printf("warning: could not watch agent dir %s: %v", agentDir, err)
			}
		}
	}

	go m.watchLoop()

	return m, nil
}

// Get returns an agent by name, or nil if not found.
func (m *Manager) Get(name string) *Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if e := m.entries[name]; e != nil {
		return e.agent
	}
	return nil
}

// Runtime returns the AgentRuntime for the given agent name, or nil if not found.
func (m *Manager) Runtime(name string) *AgentRuntime {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if e := m.entries[name]; e != nil {
		return e.runtime
	}
	return nil
}

// All returns a snapshot of all loaded agents.
func (m *Manager) All() map[string]*Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshot := make(map[string]*Agent, len(m.entries))
	for k, e := range m.entries {
		snapshot[k] = e.agent
	}
	return snapshot
}

// Close stops the fsnotify watcher and all agent runtimes.
func (m *Manager) Close() error {
	m.cancel()
	if m.watcher != nil {
		return m.watcher.Close()
	}
	return nil
}

func (m *Manager) loadAll() error {
	defs, err := config.LoadAgents(m.paths.AgentsConfigDir(), m.server)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for name, def := range defs {
		agent, err := NewAgent(def)
		if err != nil {
			log.Printf("warning: failed to parse permissions for agent %q: %v", name, err)
			continue
		}
		m.entries[name] = m.newEntry(agent)
	}

	return nil
}

func (m *Manager) watchLoop() {
	for {
		select {
		case event, ok := <-m.watcher.Events:
			if !ok {
				return
			}
			m.handleEvent(event)

		case err, ok := <-m.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("fsnotify error: %v", err)
		}
	}
}

func (m *Manager) handleEvent(event fsnotify.Event) {
	// Only care about agent.yaml files
	if filepath.Base(event.Name) != "agent.yaml" {
		return
	}

	// Extract agent name from path: .../agents/<name>/agent.yaml
	rel, err := filepath.Rel(m.paths.AgentsConfigDir(), event.Name)
	if err != nil {
		return
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 1 {
		return
	}
	agentName := parts[0]

	if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
		if err := m.reloadAgent(agentName); err != nil {
			log.Printf("warning: failed to reload agent %q: %v", agentName, err)
		} else {
			log.Printf("reloaded agent %q", agentName)
		}
	} else if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		m.mu.Lock()
		if e, ok := m.entries[agentName]; ok {
			e.agent.Deactivate()
			log.Printf("deactivated agent %q (file removed)", agentName)
		}
		m.mu.Unlock()
	}
}

// ConfirmationRegistry returns the shared in-memory registry of pending confirmations.
// Used by the API layer to serve GET /api/rooms/{id}/confirmations and WS replays.
func (m *Manager) ConfirmationRegistry() *ConfirmationRegistry {
	return m.confirmationRegistry
}

// newEntry creates an agentEntry and starts its runtime goroutine if deps are configured.
// Must be called with m.mu held.
func (m *Manager) newEntry(agent *Agent) *agentEntry {
	e := &agentEntry{agent: agent}
	if m.registry != nil {
		e.runtime = NewAgentRuntime(agent, RuntimeDeps{
			Registry:             m.registry,
			Assembler:            m.assembler,
			Executor:             m.executor,
			ConfirmationRegistry: m.confirmationRegistry,
			Notifier:             m.notifier,
			ResponseWriter:       m.responseWriter,
		})
		go e.runtime.Run(m.ctx)
	}
	return e
}

func (m *Manager) reloadAgent(name string) error {
	agentDir := filepath.Join(m.paths.AgentsConfigDir(), name)
	def, err := config.LoadAgents(agentDir, m.server)
	if err != nil {
		return err
	}

	newDef, ok := def[name]
	if !ok {
		return fmt.Errorf("agent %q not found in %s", name, agentDir)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if e, ok := m.entries[name]; ok {
		if err := e.agent.UpdateDefinition(newDef); err != nil {
			return err
		}
	} else {
		agent, err := NewAgent(newDef)
		if err != nil {
			return err
		}
		m.entries[name] = m.newEntry(agent)
		// Watch the new agent directory
		if m.watcher != nil {
			if err := m.watcher.Add(agentDir); err != nil {
				log.Printf("warning: could not watch new agent dir %s: %v", agentDir, err)
			}
		}
	}

	return nil
}
