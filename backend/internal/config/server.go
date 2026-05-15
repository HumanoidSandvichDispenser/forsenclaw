package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Listen     string           `yaml:"listen"`
	Providers  []Provider       `yaml:"providers"`
	Models     map[string]Model `yaml:"models"`
	Embeddings EmbeddingsConfig `yaml:"embeddings"`
}

// EmbeddingsConfig configures the embedding provider for the search index.
type EmbeddingsConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	BaseURL  string `yaml:"base_url,omitempty"`
}

type Provider struct {
	Name      string `yaml:"name"`
	Protocol  string `yaml:"protocol"`
	BaseURL   string `yaml:"base_url"`
	APIKeyEnv string `yaml:"api_key_env"`
	APIKey    string `yaml:"api_key,omitempty"`
}

type Model struct {
	Provider      string `yaml:"provider"`
	ProviderModel string `yaml:"provider_model"`
}

func LoadServerConfig(path string) (*ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading server config: %w", err)
	}

	var cfg ServerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing server config: %w", err)
	}

	if errs := ValidateServerConfig(&cfg); len(errs) > 0 {
		return nil, fmt.Errorf("invalid server config: %v", errs)
	}

	return &cfg, nil
}

func (c *ServerConfig) ProviderByName(name string) *Provider {
	for i := range c.Providers {
		if c.Providers[i].Name == name {
			return &c.Providers[i]
		}
	}
	return nil
}
