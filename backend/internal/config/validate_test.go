package config

import (
	"testing"
)

func TestValidateServerConfig_valid(t *testing.T) {
	cfg := &ServerConfig{
		Listen: ":8080",
		Providers: []Provider{
			{Name: "anthropic", Protocol: "anthropic", BaseURL: "https://api.anthropic.com", APIKeyEnv: "ANTHROPIC_API_KEY"},
			{Name: "ollama", Protocol: "openai_compatible", BaseURL: "http://localhost:11434"},
		},
		Models: map[string]Model{
			"claude-sonnet-4.6": {Provider: "anthropic", ProviderModel: "claude-sonnet-4-20250514"},
			"gemma-4-local":     {Provider: "ollama", ProviderModel: "gemma3:12b"},
		},
	}

	errs := ValidateServerConfig(cfg)
	if len(errs) > 0 {
		t.Errorf("valid config returned errors: %v", errs)
	}
}

func TestValidateServerConfig_emptyListen(t *testing.T) {
	cfg := &ServerConfig{
		Listen: "",
	}

	errs := ValidateServerConfig(cfg)
	if len(errs) == 0 {
		t.Fatal("expected errors for empty listen, got none")
	}
	found := false
	for _, e := range errs {
		if e.Field == "listen" {
			found = true
		}
	}
	if !found {
		t.Error("expected error on listen field")
	}
}

func TestValidateServerConfig_duplicateProviders(t *testing.T) {
	cfg := &ServerConfig{
		Listen: ":8080",
		Providers: []Provider{
			{Name: "dup", Protocol: "anthropic", BaseURL: "https://a.com"},
			{Name: "dup", Protocol: "anthropic", BaseURL: "https://b.com"},
		},
	}

	errs := ValidateServerConfig(cfg)
	found := false
	for _, e := range errs {
		if e.Field == "providers[1].name" {
			found = true
		}
	}
	if !found {
		t.Error("expected duplicate provider name error")
	}
}

func TestValidateServerConfig_unknownProvider(t *testing.T) {
	cfg := &ServerConfig{
		Listen:    ":8080",
		Providers: []Provider{},
		Models: map[string]Model{
			"test": {Provider: "nonexistent", ProviderModel: "fake"},
		},
	}

	errs := ValidateServerConfig(cfg)
	found := false
	for _, e := range errs {
		if e.Field == "models[test].provider" {
			found = true
		}
	}
	if !found {
		t.Error("expected unknown provider error")
	}
}

func TestValidateServerConfig_invalidProtocol(t *testing.T) {
	cfg := &ServerConfig{
		Listen: ":8080",
		Providers: []Provider{
			{Name: "badproto", Protocol: "invalid", BaseURL: "https://example.com"},
		},
	}

	errs := ValidateServerConfig(cfg)
	found := false
	for _, e := range errs {
		if e.Field == "providers[0].protocol" {
			found = true
		}
	}
	if !found {
		t.Error("expected invalid protocol error")
	}
}

func TestValidateAgentDefinition_valid(t *testing.T) {
	serverCfg := &ServerConfig{
		Models: map[string]Model{
			"gemma-4-local": {Provider: "ollama", ProviderModel: "gemma3:12b"},
		},
	}

	agent := &AgentDefinition{
		Name:            "housewife",
		RoleDescription: "Personal agent",
		Models:          AgentModels{Primary: "gemma-4-local", Routine: "gemma-4-local", Sensitive: "gemma-4-local"},
		Clearance:       5,
		RawPermissions:  []string{"room:create", "tool:invoke[*]"},
	}

	errs := ValidateAgentDefinition(agent, serverCfg)
	if len(errs) > 0 {
		t.Errorf("valid agent returned errors: %v", errs)
	}
}

func TestValidateAgentDefinition_missingFields(t *testing.T) {
	agent := &AgentDefinition{
		Name:            "",
		RoleDescription: "",
		Clearance:       0,
	}

	errs := ValidateAgentDefinition(agent, nil)
	if len(errs) < 3 {
		t.Errorf("expected at least 3 errors for empty agent, got %d: %v", len(errs), errs)
	}
}

func TestValidateAgentDefinition_unknownModel(t *testing.T) {
	serverCfg := &ServerConfig{
		Models: map[string]Model{
			"gemma-4-local": {Provider: "ollama", ProviderModel: "gemma3:12b"},
		},
	}

	agent := &AgentDefinition{
		Name:            "bad",
		RoleDescription: "test",
		Models:          AgentModels{Primary: "nonexistent", Routine: "gemma-4-local", Sensitive: "gemma-4-local"},
		Clearance:       1,
		RawPermissions:  []string{},
	}

	errs := ValidateAgentDefinition(agent, serverCfg)
	found := false
	for _, e := range errs {
		if e.Field == "models.primary" {
			found = true
		}
	}
	if !found {
		t.Error("expected unknown model error for models.primary")
	}
}

func TestValidateAgentDefinition_badName(t *testing.T) {
	agent := &AgentDefinition{
		Name:            "evil/../path",
		RoleDescription: "test",
		Models:          AgentModels{Primary: "m", Routine: "m", Sensitive: "m"},
		Clearance:       1,
	}

	errs := ValidateAgentDefinition(agent, nil)
	found := false
	for _, e := range errs {
		if e.Field == "name" {
			found = true
		}
	}
	if !found {
		t.Error("expected bad name error")
	}
}

func TestValidateAgentDefinition_badPermission(t *testing.T) {
	agent := &AgentDefinition{
		Name:            "test",
		RoleDescription: "test",
		Models:          AgentModels{Primary: "m", Routine: "m", Sensitive: "m"},
		Clearance:       1,
		RawPermissions:  []string{""},
	}

	errs := ValidateAgentDefinition(agent, nil)
	if len(errs) == 0 {
		t.Error("expected error for empty permission string")
	}
}
