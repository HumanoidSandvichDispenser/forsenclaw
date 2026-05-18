package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadServerConfig_valid(t *testing.T) {
	path := filepath.Join("testdata", "valid_hearth.yaml")
	cfg, err := LoadServerConfig(path)
	if err != nil {
		t.Fatalf("LoadServerConfig returned error: %v", err)
	}

	if cfg.Listen != ":8080" {
		t.Errorf("Listen = %q, want :8080", cfg.Listen)
	}
	if len(cfg.Providers) != 2 {
		t.Errorf("len(Providers) = %d, want 2", len(cfg.Providers))
	}
	if cfg.Providers[0].Name != "anthropic" {
		t.Errorf("Provider[0].Name = %q, want anthropic", cfg.Providers[0].Name)
	}
	if cfg.Providers[0].Protocol != "anthropic" {
		t.Errorf("Provider[0].Protocol = %q, want anthropic", cfg.Providers[0].Protocol)
	}
	if string(cfg.Providers[0].APIKey) != "${ANTHROPIC_API_KEY}" {
		t.Errorf("Provider[0].APIKey = %q, want ${ANTHROPIC_API_KEY}", cfg.Providers[0].APIKey)
	}
	t.Setenv("ANTHROPIC_API_KEY", "resolved-key")
	if got := cfg.Providers[0].APIKey.Resolve(); got != "resolved-key" {
		t.Errorf("Provider[0].APIKey.Resolve() = %q, want resolved-key", got)
	}
	if cfg.Providers[1].Name != "ollama" {
		t.Errorf("Provider[1].Name = %q, want ollama", cfg.Providers[1].Name)
	}
	if cfg.Providers[1].Protocol != "openai_compatible" {
		t.Errorf("Provider[1].Protocol = %q, want openai_compatible", cfg.Providers[1].Protocol)
	}

	if len(cfg.Models) != 2 {
		t.Errorf("len(Models) = %d, want 2", len(cfg.Models))
	}
	if m, ok := cfg.Models["claude-sonnet-4.6"]; !ok {
		t.Error("missing model claude-sonnet-4.6")
	} else if m.Provider != "anthropic" {
		t.Errorf("claude-sonnet-4.6 provider = %q, want anthropic", m.Provider)
	}
	if m, ok := cfg.Models["gemma-4-local"]; !ok {
		t.Error("missing model gemma-4-local")
	} else if m.Provider != "ollama" {
		t.Errorf("gemma-4-local provider = %q, want ollama", m.Provider)
	}
}

func TestEnvStringResolve(t *testing.T) {
	t.Setenv("API_KEY_FROM_ENV", "secret")

	tests := []struct {
		name string
		key  EnvString
		want string
	}{
		{name: "literal", key: EnvString("abc123"), want: "abc123"},
		{name: "env", key: EnvString("${API_KEY_FROM_ENV}"), want: "secret"},
		{name: "fallback", key: EnvString("${MISSING_KEY:-fallback}"), want: "fallback"},
		{name: "missing", key: EnvString("${MISSING_KEY}"), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.key.Resolve(); got != tt.want {
				t.Fatalf("Resolve() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadServerConfig_invalid(t *testing.T) {
	path := filepath.Join("testdata", "invalid_hearth.yaml")
	_, err := LoadServerConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid config, got nil")
	}
}

func TestLoadServerConfig_missing(t *testing.T) {
	_, err := LoadServerConfig("/nonexistent/path.yaml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestServerConfig_ProviderByName(t *testing.T) {
	cfg := &ServerConfig{
		Providers: []Provider{
			{Name: "anthropic", Protocol: "anthropic", BaseURL: "https://api.anthropic.com"},
			{Name: "ollama", Protocol: "openai_compatible", BaseURL: "http://localhost:11434"},
		},
	}

	if p := cfg.ProviderByName("anthropic"); p == nil || p.BaseURL != "https://api.anthropic.com" {
		t.Error("ProviderByName(anthropic) failed")
	}
	if p := cfg.ProviderByName("missing"); p != nil {
		t.Error("ProviderByName(missing) should return nil")
	}
}

func TestLoadServerConfig_fromTempDir(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`
listen: ":9090"
providers:
  - name: testprov
    protocol: openai_compatible
    base_url: http://localhost:11434
models:
  testmodel:
    provider: testprov
    provider_model: test:1b
`)
	path := filepath.Join(dir, "hearth.yaml")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadServerConfig(path)
	if err != nil {
		t.Fatalf("LoadServerConfig returned error: %v", err)
	}
	if cfg.Listen != ":9090" {
		t.Errorf("Listen = %q, want :9090", cfg.Listen)
	}
}
