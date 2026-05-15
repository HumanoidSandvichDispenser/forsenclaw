package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
)

func TestNewAgent(t *testing.T) {
	def := &config.AgentDefinition{
		Name:            "test",
		RoleDescription: "test agent",
		Clearance:       3,
		RawPermissions:  []string{"room:create"},
	}

	agent, err := NewAgent(def)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}

	if agent.Name() != "test" {
		t.Fatalf("expected name 'test', got %q", agent.Name())
	}
	if !agent.IsActive() {
		t.Fatal("expected agent to be active")
	}
	if len(agent.Permissions()) != 1 {
		t.Fatalf("expected 1 permission, got %d", len(agent.Permissions()))
	}
}

func TestAgentUpdateDefinition(t *testing.T) {
	def := &config.AgentDefinition{
		Name:            "test",
		RoleDescription: "test agent",
		Clearance:       3,
		RawPermissions:  []string{"room:create"},
	}

	agent, _ := NewAgent(def)
	oldLoadedAt := agent.LoadedAt
	time.Sleep(10 * time.Millisecond)

	newDef := &config.AgentDefinition{
		Name:            "test",
		RoleDescription: "updated agent",
		Clearance:       5,
		RawPermissions:  []string{"room:create", "memory:write[*]"},
	}

	if err := agent.UpdateDefinition(newDef); err != nil {
		t.Fatalf("UpdateDefinition failed: %v", err)
	}

	if agent.Definition.RoleDescription != "updated agent" {
		t.Fatalf("expected updated role, got %q", agent.Definition.RoleDescription)
	}
	if agent.Definition.Clearance != 5 {
		t.Fatalf("expected clearance 5, got %d", agent.Definition.Clearance)
	}
	if len(agent.Permissions()) != 2 {
		t.Fatalf("expected 2 permissions, got %d", len(agent.Permissions()))
	}
	if !agent.LoadedAt.After(oldLoadedAt) {
		t.Fatal("expected LoadedAt to be updated")
	}
}

func TestAgentDeactivate(t *testing.T) {
	def := &config.AgentDefinition{
		Name:            "test",
		RoleDescription: "test agent",
		Clearance:       3,
	}

	agent, _ := NewAgent(def)
	agent.Deactivate()
	if agent.IsActive() {
		t.Fatal("expected agent to be inactive")
	}
}

func TestManagerLoadAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	p := paths.NewPathsFromRoots(tmpDir+"/config", tmpDir+"/data", tmpDir+"/cache")

	// Create agent config
	agentDir := filepath.Join(p.AgentsConfigDir(), "housewife")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("creating agent dir: %v", err)
	}

	agentYAML := `name: housewife
role_description: test agent
models:
  primary: gemma
  routine: gemma
  sensitive: gemma
feature_flags:
  identity_continuity: true
  daily_notes: true
  proactive_triggers: true
  dreaming: true
clearance: 5
permissions:
  - room:create
`
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), []byte(agentYAML), 0644); err != nil {
		t.Fatalf("writing agent.yaml: %v", err)
	}

	serverCfg := &config.ServerConfig{
		Listen: ":8080",
		Providers: []config.Provider{
			{Name: "ollama", Protocol: "openai_compatible", BaseURL: "http://localhost:11434"},
		},
		Models: map[string]config.Model{
			"gemma": {Provider: "ollama", ProviderModel: "gemma3:12b"},
		},
	}

	mgr, err := NewManager(p, serverCfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	// Should load the agent
	a := mgr.Get("housewife")
	if a == nil {
		t.Fatal("expected agent 'housewife' to be loaded")
	}
	if a.Name() != "housewife" {
		t.Fatalf("expected name 'housewife', got %q", a.Name())
	}

	// Unknown agent
	if mgr.Get("unknown") != nil {
		t.Fatal("expected nil for unknown agent")
	}

	// All snapshot
	all := mgr.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(all))
	}
}
