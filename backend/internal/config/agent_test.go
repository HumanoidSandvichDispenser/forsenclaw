package config

import (
	"testing"
)

func TestParseStatement(t *testing.T) {
	tests := []struct {
		raw         string
		wantActions []string
		wantRes     []string
		wantEffect  string
		wantErr     bool
	}{
		{"room:create", []string{"room:create"}, []string{"**"}, "allow", false},
		{"tool:invoke/**", []string{"tool:invoke"}, []string{"**"}, "allow", false},
		{"tool:invoke/frsn:tool/**", []string{"tool:invoke"}, []string{"frsn:tool/**"}, "allow", false},
		{"tool:invoke/frsn:tool/builtin/*:require_confirmation", []string{"tool:invoke"}, []string{"frsn:tool/builtin/*"}, "require_confirmation", false},
		{"tool:invoke/frsn:tool/builtin/*:deny", []string{"tool:invoke"}, []string{"frsn:tool/builtin/*"}, "deny", false},
		{"", nil, nil, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			s, err := ParseStatement(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseStatement(%q) expected error, got nil", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseStatement(%q) unexpected error: %v", tt.raw, err)
			}
			if len(s.Actions) != len(tt.wantActions) || s.Actions[0] != tt.wantActions[0] {
				t.Errorf("Actions = %v, want %v", s.Actions, tt.wantActions)
			}
			if len(s.Resources) != len(tt.wantRes) || s.Resources[0] != tt.wantRes[0] {
				t.Errorf("Resources = %v, want %v", s.Resources, tt.wantRes)
			}
			if s.Effect != tt.wantEffect {
				t.Errorf("Effect = %q, want %q", s.Effect, tt.wantEffect)
			}
		})
	}
}

func TestStatement_Matches(t *testing.T) {
	tests := []struct {
		name     string
		stmt     Statement
		action   string
		resource string
		want     bool
	}{
		{
			"wildcard resource matches anything",
			Statement{Actions: []string{"tool:invoke"}, Resources: []string{"**"}, Effect: "allow"},
			"tool:invoke", "frsn:tool/builtin/webfetch", true,
		},
		{
			"frsn double-star matches all tools",
			Statement{Actions: []string{"tool:invoke"}, Resources: []string{"frsn:tool/**"}, Effect: "allow"},
			"tool:invoke", "frsn:tool/builtin/webfetch", true,
		},
		{
			"frsn server glob matches tool on that server",
			Statement{Actions: []string{"tool:invoke"}, Resources: []string{"frsn:tool/builtin/*"}, Effect: "allow"},
			"tool:invoke", "frsn:tool/builtin/webfetch", true,
		},
		{
			"frsn server glob no match on different server",
			Statement{Actions: []string{"tool:invoke"}, Resources: []string{"frsn:tool/builtin/*"}, Effect: "allow"},
			"tool:invoke", "frsn:tool/mcp/filesystem/read_file", false,
		},
		{
			"action mismatch",
			Statement{Actions: []string{"room:create"}, Resources: []string{"**"}, Effect: "allow"},
			"tool:invoke", "frsn:tool/builtin/webfetch", false,
		},
		{
			"exact resource match",
			Statement{Actions: []string{"tool:invoke"}, Resources: []string{"frsn:tool/builtin/webfetch"}, Effect: "allow"},
			"tool:invoke", "frsn:tool/builtin/webfetch", true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.stmt.Matches(tt.action, tt.resource); got != tt.want {
				t.Errorf("Matches(%q, %q) = %v, want %v", tt.action, tt.resource, got, tt.want)
			}
		})
	}
}
