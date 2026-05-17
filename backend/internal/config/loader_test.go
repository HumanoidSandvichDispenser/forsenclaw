package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAgents_valid(t *testing.T) {
	serverCfg := &ServerConfig{
		Models: map[string]Model{
			"gemma-4-local":     {Provider: "ollama", ProviderModel: "gemma3:12b"},
			"claude-sonnet-4.6": {Provider: "anthropic", ProviderModel: "claude-sonnet-4-20250514"},
		},
	}

	dir := t.TempDir()
	agentDir := filepath.Join(dir, "housewife")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}

	agentYAML := []byte(`
name: housewife
role_description: "Personal agent"
memory_budget: 8192
models:
  primary: gemma-4-local
  routine: gemma-4-local
  sensitive: gemma-4-local
feature_flags:
  identity_continuity: true
  daily_notes: true
  proactive_triggers: true
  dreaming: true
clearance: 5
permissions:
  - room:create
  - tool:invoke[*]
`)
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), agentYAML, 0644); err != nil {
		t.Fatal(err)
	}

	agents, err := LoadAgents(dir, serverCfg)
	if err != nil {
		t.Fatalf("LoadAgents returned error: %v", err)
	}

	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents["housewife"] == nil {
		t.Fatal("expected housewife agent")
	}
	if agents["housewife"].Clearance != 5 {
		t.Errorf("Clearance = %d, want 5", agents["housewife"].Clearance)
	}
	if agents["housewife"].Models.Primary != "gemma-4-local" {
		t.Errorf("Primary = %q, want gemma-4-local", agents["housewife"].Models.Primary)
	}
	if agents["housewife"].MemoryBudget != 8192 {
		t.Errorf("MemoryBudget = %d, want 8192", agents["housewife"].MemoryBudget)
	}
}

func TestLoadAgents_empty(t *testing.T) {
	dir := t.TempDir()
	serverCfg := &ServerConfig{Models: map[string]Model{}}

	agents, err := LoadAgents(dir, serverCfg)
	if err != nil {
		t.Fatalf("LoadAgents on empty dir returned error: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

func TestLoadAgents_missingDir(t *testing.T) {
	serverCfg := &ServerConfig{Models: map[string]Model{}}

	agents, err := LoadAgents("/nonexistent", serverCfg)
	if err != nil {
		t.Fatalf("LoadAgents on missing dir returned error: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

func TestLoadAgents_unknownModel(t *testing.T) {
	serverCfg := &ServerConfig{
		Models: map[string]Model{
			"gemma-4-local": {Provider: "ollama", ProviderModel: "gemma3:12b"},
		},
	}

	dir := t.TempDir()
	agentDir := filepath.Join(dir, "badagent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}

	agentYAML := []byte(`
name: badagent
role_description: "test"
models:
  primary: nonexistent-model
  routine: gemma-4-local
  sensitive: gemma-4-local
clearance: 1
permissions: []
`)
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), agentYAML, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadAgents(dir, serverCfg)
	if err == nil {
		t.Fatal("expected error for unknown model reference, got nil")
	}
}

func TestLoadAgents_nameMismatch(t *testing.T) {
	serverCfg := &ServerConfig{Models: map[string]Model{}}

	dir := t.TempDir()
	agentDir := filepath.Join(dir, "dirname")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}

	agentYAML := []byte(`
name: wrongname
role_description: "test"
models:
  primary: m
  routine: m
  sensitive: m
clearance: 1
permissions: []
`)
	if err := os.WriteFile(filepath.Join(agentDir, "agent.yaml"), agentYAML, 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadAgents(dir, serverCfg)
	if err == nil {
		t.Fatal("expected error for name mismatch, got nil")
	}
}
