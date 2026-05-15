package agent

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
)

// Manager loads, tracks, and hot-reloads agent definitions from disk.
type Manager struct {
	mu      sync.RWMutex
	agents  map[string]*Agent
	watcher *fsnotify.Watcher
	paths   *paths.Paths
	server  *config.ServerConfig
}

// NewManager creates a manager and loads all agents from disk.
func NewManager(p *paths.Paths, serverCfg *config.ServerConfig) (*Manager, error) {
	m := &Manager{
		agents: make(map[string]*Agent),
		paths:  p,
		server: serverCfg,
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
	return m.agents[name]
}

// All returns a snapshot of all loaded agents.
func (m *Manager) All() map[string]*Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshot := make(map[string]*Agent, len(m.agents))
	for k, v := range m.agents {
		snapshot[k] = v
	}
	return snapshot
}

// Close stops the fsnotify watcher.
func (m *Manager) Close() error {
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
		m.agents[name] = agent
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
		if agent, ok := m.agents[agentName]; ok {
			agent.Deactivate()
			log.Printf("deactivated agent %q (file removed)", agentName)
		}
		m.mu.Unlock()
	}
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

	if agent, ok := m.agents[name]; ok {
		if err := agent.UpdateDefinition(newDef); err != nil {
			return err
		}
	} else {
		agent, err := NewAgent(newDef)
		if err != nil {
			return err
		}
		m.agents[name] = agent
		// Watch the new agent directory
		if m.watcher != nil {
			if err := m.watcher.Add(agentDir); err != nil {
				log.Printf("warning: could not watch new agent dir %s: %v", agentDir, err)
			}
		}
	}

	return nil
}
