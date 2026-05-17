package config

import (
	"fmt"
	"net/url"
	"strings"
)

type ConfigError struct {
	Field   string
	Message string
}

func (e ConfigError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s", e.Field, e.Message)
	}
	return e.Message
}

func ValidateServerConfig(cfg *ServerConfig) []ConfigError {
	var errs []ConfigError

	if cfg.Listen == "" {
		errs = append(errs, ConfigError{Field: "listen", Message: "must not be empty"})
	}

	providerNames := make(map[string]int)
	for i, p := range cfg.Providers {
		if p.Name == "" {
			errs = append(errs, ConfigError{Field: fmt.Sprintf("providers[%d].name", i), Message: "must not be empty"})
		} else {
			if providerNames[p.Name] > 0 {
				errs = append(errs, ConfigError{Field: fmt.Sprintf("providers[%d].name", i), Message: fmt.Sprintf("duplicate provider name %q", p.Name)})
			}
			providerNames[p.Name]++
		}

		validProtocols := map[string]bool{"anthropic": true, "openai_compatible": true}
		if !validProtocols[p.Protocol] {
			errs = append(errs, ConfigError{Field: fmt.Sprintf("providers[%d].protocol", i), Message: fmt.Sprintf("invalid protocol %q, must be one of: anthropic, openai_compatible", p.Protocol)})
		}

		if p.BaseURL == "" {
			errs = append(errs, ConfigError{Field: fmt.Sprintf("providers[%d].base_url", i), Message: "must not be empty"})
		}
	}

	for modelKey, m := range cfg.Models {
		if m.Provider == "" {
			errs = append(errs, ConfigError{Field: fmt.Sprintf("models[%s].provider", modelKey), Message: "must not be empty"})
		} else if providerNames[m.Provider] == 0 {
			errs = append(errs, ConfigError{Field: fmt.Sprintf("models[%s].provider", modelKey), Message: fmt.Sprintf("references unknown provider %q", m.Provider)})
		}

		if m.ProviderModel == "" {
			errs = append(errs, ConfigError{Field: fmt.Sprintf("models[%s].provider_model", modelKey), Message: "must not be empty"})
		}
	}

	// Validate embeddings config (optional — only if provider is set)
	if cfg.Embeddings.Provider != "" {
		if providerNames[cfg.Embeddings.Provider] == 0 {
			errs = append(errs, ConfigError{Field: "embeddings.provider", Message: fmt.Sprintf("references unknown provider %q", cfg.Embeddings.Provider)})
		}
		if cfg.Embeddings.Model == "" {
			errs = append(errs, ConfigError{Field: "embeddings.model", Message: "must not be empty"})
		}
	}

	// Validate context config.
	// Note: LoadServerConfig applies defaults before calling this function, so
	// CompactionTrigger and CompactionTarget are always non-zero in production.
	// The relationship check is unconditional so that invalid explicit values
	// are always caught regardless of how ValidateServerConfig is called.
	if cfg.Context.CompactionTarget >= cfg.Context.CompactionTrigger {
		errs = append(errs, ConfigError{Field: "context.compaction_target", Message: "must be lower than context.compaction_trigger"})
	}
	if cfg.Context.CurrentRoomWindow < 1 {
		errs = append(errs, ConfigError{Field: "context.current_room_window", Message: "must be at least 1"})
	}
	if cfg.Context.OtherRoomWindow < 0 {
		errs = append(errs, ConfigError{Field: "context.other_room_window", Message: "must be non-negative"})
	}
	if cfg.Context.MinimumGuaranteed < 1 {
		errs = append(errs, ConfigError{Field: "context.minimum_guaranteed", Message: "must be at least 1"})
	}

	// Validate tools config.
	if cfg.Tools.MaxToolIterations < 0 {
		errs = append(errs, ConfigError{Field: "tools.max_tool_iterations", Message: "must be non-negative (0 = use default of 10)"})
	}
	for i, srv := range cfg.Tools.Servers {
		if srv.Name == "" {
			errs = append(errs, ConfigError{Field: fmt.Sprintf("tools.servers[%d].name", i), Message: "must not be empty"})
		}
		if srv.URL == "" {
			errs = append(errs, ConfigError{Field: fmt.Sprintf("tools.servers[%d].url", i), Message: "must not be empty"})
		} else if u, err := url.ParseRequestURI(srv.URL); err != nil || u.Scheme == "" || u.Host == "" {
			errs = append(errs, ConfigError{Field: fmt.Sprintf("tools.servers[%d].url", i), Message: fmt.Sprintf("invalid URL %q", srv.URL)})
		}
	}

	return errs
}

func ValidateAgentDefinition(agent *AgentDefinition, serverCfg *ServerConfig) []ConfigError {
	var errs []ConfigError

	if agent.Name == "" {
		errs = append(errs, ConfigError{Field: "name", Message: "must not be empty"})
	}

	if agent.RoleDescription == "" {
		errs = append(errs, ConfigError{Field: "role_description", Message: "must not be empty"})
	}

	if agent.Clearance <= 0 {
		errs = append(errs, ConfigError{Field: "clearance", Message: "must be a positive integer"})
	}
	if agent.MemoryBudget < 0 {
		errs = append(errs, ConfigError{Field: "memory_budget", Message: "must be non-negative (0 = use default)"})
	}

	modelRefs := []struct {
		field string
		value string
	}{
		{"models.primary", agent.Models.Primary},
		{"models.routine", agent.Models.Routine},
		{"models.sensitive", agent.Models.Sensitive},
	}

	for _, ref := range modelRefs {
		if ref.value == "" {
			errs = append(errs, ConfigError{Field: ref.field, Message: "must not be empty"})
		} else if serverCfg != nil {
			if _, ok := serverCfg.Models[ref.value]; !ok {
				errs = append(errs, ConfigError{Field: ref.field, Message: fmt.Sprintf("references unknown model %q", ref.value)})
			}
		}
	}

	for _, raw := range agent.RawPermissions {
		if _, err := ParsePermission(raw); err != nil {
			errs = append(errs, ConfigError{Field: "permissions", Message: err.Error()})
		} else {
			p, _ := ParsePermission(raw)
			validEffects := map[string]bool{"allow": true, "require_confirmation": true, "deny": true}
			if !validEffects[p.Effect] {
				errs = append(errs, ConfigError{Field: "permissions", Message: fmt.Sprintf("invalid effect %q in %q", p.Effect, raw)})
			}
		}
	}

	if strings.Contains(agent.Name, "/") || strings.Contains(agent.Name, "..") {
		errs = append(errs, ConfigError{Field: "name", Message: "must not contain / or .."})
	}

	return errs
}
