package inference

import (
	"fmt"
	"os"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
)

// Registry resolves model strings to provider adapters and provider-specific model IDs.
type Registry struct {
	// entries maps model key → resolved entry
	entries map[string]registryEntry
	// providers maps provider name → Provider adapter (for embedding lookup)
	providers map[string]Provider
}

type registryEntry struct {
	provider      Provider
	providerModel string
	providerName  string
}

// NewRegistry builds a Registry from a ServerConfig.
// It constructs provider adapters, resolves API keys from environment variables,
// and maps each model key to its adapter + provider-specific model ID.
func NewRegistry(cfg *config.ServerConfig) (*Registry, error) {
	r := &Registry{
		entries:   make(map[string]registryEntry),
		providers: make(map[string]Provider),
	}

	// Build provider adapters
	for i := range cfg.Providers {
		prov := &cfg.Providers[i]
		var adapter Provider
		var err error

		switch prov.Protocol {
		case "anthropic":
			apiKey := resolveAPIKey(prov)
			adapter, err = NewAnthropicAdapter(prov.BaseURL, apiKey)
		case "openai_compatible":
			apiKey := resolveAPIKey(prov)
			adapter, err = NewOpenAICompatibleAdapter(prov.BaseURL, apiKey)
		default:
			return nil, fmt.Errorf("unknown provider protocol %q for provider %q", prov.Protocol, prov.Name)
		}
		if err != nil {
			return nil, fmt.Errorf("building adapter for provider %q: %w", prov.Name, err)
		}

		r.providers[prov.Name] = adapter
	}

	// Map model keys to resolved entries
	for modelKey, model := range cfg.Models {
		prov, ok := r.providers[model.Provider]
		if !ok {
			return nil, fmt.Errorf("model %q references unknown provider %q", modelKey, model.Provider)
		}
		if model.ProviderModel == "" {
			return nil, fmt.Errorf("model %q has empty provider_model", modelKey)
		}
		r.entries[modelKey] = registryEntry{
			provider:      prov,
			providerModel: model.ProviderModel,
			providerName:  model.Provider,
		}
	}

	return r, nil
}

// Resolve looks up a model key and returns the provider adapter + provider-specific model ID.
func (r *Registry) Resolve(modelKey string) (Provider, string, error) {
	entry, ok := r.entries[modelKey]
	if !ok {
		return nil, "", fmt.Errorf("unknown model %q", modelKey)
	}
	return entry.provider, entry.providerModel, nil
}

// ResolveTier resolves the appropriate model for an agent's tier.
func (r *Registry) ResolveTier(agent *config.AgentDefinition, tier ModelTier) (Provider, string, error) {
	var modelKey string
	switch tier {
	case TierPrimary:
		modelKey = agent.Models.Primary
	case TierRoutine:
		modelKey = agent.Models.Routine
	case TierSensitive:
		modelKey = agent.Models.Sensitive
	default:
		return nil, "", fmt.Errorf("unknown model tier %q", tier)
	}
	if modelKey == "" {
		return nil, "", fmt.Errorf("agent %q has no model assigned for tier %q", agent.Name, tier)
	}
	return r.Resolve(modelKey)
}

// EmbeddingProvider returns the provider adapter configured for embeddings.
func (r *Registry) EmbeddingProvider(cfg config.EmbeddingsConfig) (Provider, error) {
	prov, ok := r.providers[cfg.Provider]
	if !ok {
		return nil, fmt.Errorf("embedding provider %q not found", cfg.Provider)
	}
	return prov, nil
}

// Checks the provider configuration for an API key, first looking at the
// configured APIKey field, then looking up the environment variable specified
// in APIKeyEnv. Returns an empty string if no API key is found.
func resolveAPIKey(prov *config.Provider) string {
	if prov.APIKey != "" {
		return prov.APIKey
	}

	if prov.APIKeyEnv != "" {
		return os.Getenv(prov.APIKeyEnv)
	}

	return ""
}
