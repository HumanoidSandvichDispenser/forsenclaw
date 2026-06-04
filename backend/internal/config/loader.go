package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func LoadAgents(agentsDir string, serverCfg *ServerConfig) (map[string]*AgentDefinition, error) {
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]*AgentDefinition{}, nil
		}
		return nil, fmt.Errorf("reading agents directory: %w", err)
	}

	agents := make(map[string]*AgentDefinition)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		configPath := filepath.Join(agentsDir, entry.Name(), "agent.yaml")
		data, err := os.ReadFile(configPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading agent config %s: %w", configPath, err)
		}

		var agent AgentDefinition
		if err := yaml.Unmarshal(data, &agent); err != nil {
			return nil, fmt.Errorf("parsing agent config %s: %w", configPath, err)
		}

		if errs := ValidateAgentDefinition(&agent, serverCfg); len(errs) > 0 {
			return nil, fmt.Errorf("invalid agent definition %s: %v", configPath, errs)
		}

		// Lint own permissions before flattening sets in, so a shared set's
		// mistakes aren't reported against every agent that references it.
		for _, w := range LintAgentDefinition(&agent) {
			log.Printf("warning: agent config %s: %s", configPath, w)
		}

		// Flatten referenced permission sets into the agent's grants. References
		// are already validated above, so a missing set cannot reach here.
		if serverCfg != nil {
			for _, name := range agent.PermissionSets {
				agent.Permissions = append(agent.Permissions, serverCfg.PermissionSets[name]...)
			}
		}

		if agent.Name != entry.Name() {
			return nil, fmt.Errorf("agent directory name %q does not match agent name %q", entry.Name(), agent.Name)
		}

		agents[agent.Name] = &agent
	}

	return agents, nil
}
