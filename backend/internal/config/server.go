package config

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultSystemMax is the clearance ceiling used when no clearance_levels are
// configured (the historical hardcoded value).
const DefaultSystemMax = 5

type ContextConfig struct {
	CurrentRoomWindow int `yaml:"current_room_window"`
	OtherRoomWindow   int `yaml:"other_room_window"`
	CompactionTrigger int `yaml:"compaction_trigger"`
	CompactionTarget  int `yaml:"compaction_target"`
	MinimumGuaranteed int `yaml:"minimum_guaranteed"`
}

// DefaultContextConfig returns the default context configuration.
func DefaultContextConfig() ContextConfig {
	return ContextConfig{
		CurrentRoomWindow: 50,
		OtherRoomWindow:   10,
		CompactionTrigger: 524288, // 512kb
		CompactionTarget:  262144, // 256kb
		MinimumGuaranteed: 20,
	}
}

type UserConfig struct {
	Name string `yaml:"name"`
}

type ServerConfig struct {
	Listen     string           `yaml:"listen"`
	User       UserConfig       `yaml:"user"`
	Providers  []Provider       `yaml:"providers"`
	Models     map[string]Model `yaml:"models"`
	Embeddings EmbeddingsConfig `yaml:"embeddings"`
	Context    ContextConfig    `yaml:"context"`
	Tools      ToolsConfig      `yaml:"tools"`
	Audit      AuditConfig      `yaml:"audit"`

	// ClearanceLevels names the data-classification tiers, mapping each name to
	// its integer value (e.g. {public: 0, internal: 1, confidential: 2,
	// restricted: 3}). Optional — when empty, only integer clearances are used
	// and SystemMax falls back to DefaultSystemMax.
	ClearanceLevels map[string]int `yaml:"clearance_levels,omitempty"`

	// ResourcePolicies are statements attached to resources rather than agents.
	// They can only restrict: a matching deny or require_confirmation tightens a
	// call, but they never grant access an agent's own permissions lack. Each
	// statement self-scopes through its resource patterns, so a flat list is
	// enough. Optional.
	ResourcePolicies []Statement `yaml:"resource_policies,omitempty"`

	// PermissionSets are named, reusable bundles of grant statements that agents
	// reference by name. At load each referenced set's statements are appended to
	// the agent's own permissions; they are grants like any other (a deny in a
	// set vetoes, an allow grants). Sets are flat — a set holds statements, not
	// references to other sets. Optional.
	PermissionSets map[string][]Statement `yaml:"permission_sets,omitempty"`
}

// SystemMax returns the highest configured clearance level, or DefaultSystemMax
// when no levels are configured. It is the ceiling tools fall back to when they
// declare no clearance of their own.
func (c *ServerConfig) SystemMax() int {
	max := 0
	for _, v := range c.ClearanceLevels {
		if v > max {
			max = v
		}
	}
	if max == 0 {
		return DefaultSystemMax
	}
	return max
}

// ResolveClearanceName resolves a clearance written as either a named level
// (e.g. "confidential") or a plain integer string (e.g. "2") to its integer
// value. ok is false when the string is neither a known level nor an integer.
func (c *ServerConfig) ResolveClearanceName(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if v, ok := c.ClearanceLevels[s]; ok {
		return v, true
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n, true
	}
	return 0, false
}

// AuditConfig configures the audit logging pipeline.
type AuditConfig struct {
	Sinks []AuditSinkConfig `yaml:"sinks"`
}

// AuditSinkConfig defines a single audit output destination.
type AuditSinkConfig struct {
	// Type is "console" or "sqlite".
	Type string `yaml:"type"`
	// Path is the file path for the sqlite sink. Ignored for console.
	Path string `yaml:"path,omitempty"`
	// Kinds is a list of glob patterns (e.g. "permission.*", "*") that this sink receives.
	// If empty, the sink receives all events.
	Kinds []string `yaml:"kinds"`
	// MinLevel is the minimum severity to forward to this sink ("debug", "info", "warn", "error").
	// Defaults to "info".
	MinLevel string `yaml:"min_level"`
}

// EnvString is a scalar config value that can be literal or interpolated as
// ${ENV_VAR} or ${ENV_VAR:-fallback}.
type EnvString string

var envVarRegex = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-(.*?))?\}`)

// UnmarshalYAML accepts scalar env-string values.
func (e *EnvString) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Kind == 0 {
		*e = ""
		return nil
	}
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("value must be a scalar string")
	}
	*e = EnvString(value.Value)
	return nil
}

// Resolve expands any env var interpolation and returns the resolved value.
func (e EnvString) Resolve() string {
	raw := string(e)
	if raw == "" {
		return ""
	}
	return envVarRegex.ReplaceAllStringFunc(raw, func(match string) string {
		parts := envVarRegex.FindStringSubmatch(match)
		if len(parts) < 2 {
			return ""
		}
		if val := os.Getenv(parts[1]); val != "" {
			return val
		}
		if len(parts) >= 3 && parts[2] != "" {
			return parts[2]
		}
		return ""
	})
}

// ToolsConfig configures the agentic tool-call pipeline.
type ToolsConfig struct {
	// MaxToolIterations is the hard cap on agentic loop iterations per Request.
	// Default: 10. Must be >= 1 if explicitly set.
	MaxToolIterations int `yaml:"max_tool_iterations"`

	// DefaultClearance is the fallback clearance for tools that don't specify
	// one explicitly. 0 means "system max" (the highest level defined in
	// clearance_levels). See ResolveToolClearance for resolution order.
	DefaultClearance int `yaml:"default_clearance"`

	// WebFetch configures the built-in webfetch MCP tool.
	WebFetch WebFetchConfig `yaml:"webfetch,omitempty"`

	// BraveSearch configures the built-in Brave web search MCP tool.
	BraveSearch BraveSearchConfig `yaml:"brave_search,omitempty"`

	// Servers is the list of MCP server definitions.
	Servers []MCPServerConfig `yaml:"servers"`
}

// MCPToolConfig holds common properties for all tool configurations.
// Embed this struct with `yaml:",inline"` to add clearance and future
// common fields to any tool config without duplication.
type MCPToolConfig struct {
	// Clearance is the data classification tier for this tool.
	// Higher numbers mean more sensitive data. 0 means "use default".
	Clearance int `yaml:"clearance"`
}

// BraveSearchConfig configures the built-in Brave web search MCP tool.
type BraveSearchConfig struct {
	MCPToolConfig `yaml:",inline"`
	APIKey        EnvString `yaml:"api_key,omitempty"`
}

// WebFetchConfig configures the built-in webfetch MCP tool.
type WebFetchConfig struct {
	MCPToolConfig `yaml:",inline"`
}

// MCPServerConfig defines a remote MCP server endpoint.
type MCPServerConfig struct {
	Name          string `yaml:"name"`
	URL           string `yaml:"url"`     // HTTP/SSE endpoint
	Timeout       string `yaml:"timeout"` // e.g. "30s"
	MCPToolConfig `yaml:",inline"`
}

// EmbeddingsConfig configures the embedding provider for the search index.
type EmbeddingsConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	BaseURL  string `yaml:"base_url,omitempty"`
}

type Provider struct {
	Name     string    `yaml:"name"`
	Protocol string    `yaml:"protocol"`
	BaseURL  string    `yaml:"base_url"`
	APIKey   EnvString `yaml:"api_key,omitempty"`
	ToolMode string    `yaml:"tool_mode,omitempty"`
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

	// Apply defaults for zero values in context config
	defaults := DefaultContextConfig()
	if cfg.Context.CurrentRoomWindow == 0 {
		cfg.Context.CurrentRoomWindow = defaults.CurrentRoomWindow
	}
	if cfg.Context.OtherRoomWindow == 0 {
		cfg.Context.OtherRoomWindow = defaults.OtherRoomWindow
	}
	if cfg.Context.CompactionTrigger == 0 {
		cfg.Context.CompactionTrigger = defaults.CompactionTrigger
	}
	if cfg.Context.CompactionTarget == 0 {
		cfg.Context.CompactionTarget = defaults.CompactionTarget
	}
	if cfg.Context.MinimumGuaranteed == 0 {
		cfg.Context.MinimumGuaranteed = defaults.MinimumGuaranteed
	}

	// Apply tools defaults.
	if cfg.Tools.MaxToolIterations == 0 {
		cfg.Tools.MaxToolIterations = 10
	}
	if cfg.Tools.DefaultClearance < 0 {
		cfg.Tools.DefaultClearance = 0
	}

	if errs := ValidateServerConfig(&cfg); len(errs) > 0 {
		return nil, fmt.Errorf("invalid server config: %v", errs)
	}

	for _, w := range LintServerConfig(&cfg) {
		log.Printf("warning: server config: %s", w)
	}

	return &cfg, nil
}

// ResolveToolClearance returns the effective clearance for a tool given the
// parsed config. Resolution order:
//  1. The tool's own configured Clearance (if > 0).
//  2. ToolsConfig.DefaultClearance (if > 0).
//  3. The provided systemMax fallback.
func (c *ServerConfig) ResolveToolClearance(toolClearance int, systemMax int) int {
	if toolClearance > 0 {
		return toolClearance
	}
	if c.Tools.DefaultClearance > 0 {
		return c.Tools.DefaultClearance
	}
	return systemMax
}

func (c *ServerConfig) ProviderByName(name string) *Provider {
	for i := range c.Providers {
		if c.Providers[i].Name == name {
			return &c.Providers[i]
		}
	}
	return nil
}
