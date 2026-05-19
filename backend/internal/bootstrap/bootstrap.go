package bootstrap

import (
	"os"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
)

const defaultConfig = `# Hearth server configuration

listen: ":8080"

providers: []
#  - name: anthropic
#    protocol: anthropic
#    base_url: https://api.anthropic.com
#    api_key: "${ANTHROPIC_API_KEY}"
#    # api_key: "${ANTHROPIC_API_KEY:-fallback}"
#  - name: ollama
#    protocol: openai_compatible
#    base_url: http://localhost:11434
#    # tool_mode: xml    # uncomment for local models with no native tool support

models: {}
#  claude-sonnet-4.6:
#    provider: anthropic
#    provider_model: claude-sonnet-4-20250514
#  gemma-4-local:
#    provider: ollama
#    provider_model: gemma3:12b

tools:
#  brave_search:
#    api_key: ${BRAVE_API_KEY}
`

func Bootstrap(p *paths.Paths) error {
	for _, dir := range p.AllDirs() {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	cfgPath := p.ServerConfigFile()
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		if err := os.WriteFile(cfgPath, []byte(defaultConfig), 0644); err != nil {
			return err
		}
	}

	return nil
}
