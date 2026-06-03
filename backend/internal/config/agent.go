package config

import (
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

type AgentDefinition struct {
	Name            string       `yaml:"name"`
	RoleDescription string       `yaml:"role_description"`
	Models          AgentModels  `yaml:"models"`
	FeatureFlags    FeatureFlags `yaml:"feature_flags"`
	Clearance       int          `yaml:"clearance"`
	MemoryBudget    int          `yaml:"memory_budget,omitempty"`
	Permissions     []Statement  `yaml:"permissions"`
	Timeout         string       `yaml:"timeout,omitempty"`
	MaxTokens       int          `yaml:"max_tokens,omitempty"`
	Temperature     *float64     `yaml:"temperature,omitempty"`
	Parent          string       `yaml:"parent,omitempty"`
}

type AgentModels struct {
	Primary   string `yaml:"primary"`
	Routine   string `yaml:"routine"`
	Sensitive string `yaml:"sensitive"`
}

type FeatureFlags struct {
	IdentityContinuity bool `yaml:"identity_continuity"`
	DailyNotes         bool `yaml:"daily_notes"`
	ProactiveTriggers  bool `yaml:"proactive_triggers"`
	Dreaming           bool `yaml:"dreaming"`
}

// Statement is a single permission entry (IAM-style).
// It may appear in YAML as a shorthand string or a structured mapping.
//
// Shorthand:  "tool:invoke/builtin/*"
//
//	"tool:invoke/daemon/homeserver/*:require_confirmation"
//
// Structured: effect: allow
//
//	actions: [tool:invoke]
//	resources: [builtin/*, mcp/filesystem/*]
type Statement struct {
	Effect    string   `yaml:"effect"`    // "allow" (default), "deny", "require_confirmation"
	Actions   []string `yaml:"actions"`   // e.g. ["tool:invoke"]
	Resources []string `yaml:"resources"` // e.g. ["builtin/*"], "**" = any
}

// ParseStatement parses the shorthand string permission format:
//
//	action/resource[:effect]
//
// The resource portion supports FRSN patterns (e.g. frsn:tool/**).
// "**" matches any resource path; "*" matches a single path segment.
// Effect defaults to "allow" if omitted.
func ParseStatement(raw string) (Statement, error) {
	if raw == "" {
		return Statement{}, fmt.Errorf("empty permission string")
	}

	effect := "allow"
	s := raw

	// Detect optional :effect suffix (last colon-separated segment).
	if idx := strings.LastIndex(s, ":"); idx != -1 {
		candidate := s[idx+1:]
		if candidate == "allow" || candidate == "require_confirmation" || candidate == "deny" {
			effect = candidate
			s = s[:idx]
		}
	}

	// Split action / resource on the first "/".
	action := s
	resource := "**"
	if idx := strings.Index(s, "/"); idx != -1 {
		action = s[:idx]
		resource = s[idx+1:]
	}

	if action == "" {
		return Statement{}, fmt.Errorf("empty action in permission %q", raw)
	}

	return Statement{
		Effect:    effect,
		Actions:   []string{action},
		Resources: []string{resource},
	}, nil
}

// Matches reports whether this statement applies to the given action and resource.
// Resource patterns support "**" (any path, including across segments) and "*"
// (any single segment). Other patterns use path.Match semantics.
func (s Statement) Matches(action, resource string) bool {
	actionMatched := false
	for _, a := range s.Actions {
		if matched, _ := path.Match(a, action); matched {
			actionMatched = true
			break
		}
	}
	if !actionMatched {
		return false
	}
	for _, r := range s.Resources {
		if matchGlob(r, resource) {
			return true
		}
	}
	return false
}

// matchGlob matches a resource pattern against a resource string.
// "**" matches any sequence of path segments (including none).
// Other patterns delegate to path.Match.
func matchGlob(pattern, s string) bool {
	if pattern == "**" {
		return true
	}
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return s == prefix || strings.HasPrefix(s, prefix+"/")
	}
	matched, _ := path.Match(pattern, s)
	return matched
}

// UnmarshalYAML handles both shorthand string and structured mapping forms.
func (s *Statement) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		stmt, err := ParseStatement(value.Value)
		if err != nil {
			return err
		}
		*s = stmt
		return nil
	case yaml.MappingNode:
		type statementAlias Statement
		var alias statementAlias
		if err := value.Decode(&alias); err != nil {
			return err
		}
		if alias.Effect == "" {
			alias.Effect = "allow"
		}
		*s = Statement(alias)
		return nil
	default:
		return fmt.Errorf("unexpected YAML node type for permission statement")
	}
}
