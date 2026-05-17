package config

import (
	"fmt"
	"strings"
)

type AgentDefinition struct {
	Name            string       `yaml:"name"`
	RoleDescription string       `yaml:"role_description"`
	Models          AgentModels  `yaml:"models"`
	FeatureFlags    FeatureFlags `yaml:"feature_flags"`
	Clearance       int          `yaml:"clearance"`
	MemoryBudget    int          `yaml:"memory_budget,omitempty"`
	Permissions     []Permission `yaml:"-"`
	RawPermissions  []string     `yaml:"permissions"`
	Timeout         string       `yaml:"timeout,omitempty"`
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

type Permission struct {
	Action string
	Scope  string
	Effect string
}

func ParsePermission(raw string) (Permission, error) {
	effect := "allow"
	actionScope := raw

	if idx := strings.LastIndex(raw, ":"); idx != -1 {
		candidate := raw[idx+1:]
		if candidate == "allow" || candidate == "require_confirmation" || candidate == "deny" {
			effect = candidate
			actionScope = raw[:idx]
		}
	}

	action := actionScope
	scope := ""

	if start := strings.Index(actionScope, "["); start != -1 {
		if end := strings.Index(actionScope, "]"); end != -1 && end > start {
			action = actionScope[:start]
			scope = actionScope[start+1 : end]
		}
	}

	if action == "" {
		return Permission{}, fmt.Errorf("empty action in permission %q", raw)
	}

	return Permission{
		Action: action,
		Scope:  scope,
		Effect: effect,
	}, nil
}

func (a *AgentDefinition) ParsedPermissions() ([]Permission, error) {
	perms := make([]Permission, 0, len(a.RawPermissions))
	for _, raw := range a.RawPermissions {
		p, err := ParsePermission(raw)
		if err != nil {
			return nil, fmt.Errorf("parsing permission %q for agent %q: %w", raw, a.Name, err)
		}
		perms = append(perms, p)
	}
	return perms, nil
}
