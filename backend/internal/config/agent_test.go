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
		{"tool:invoke/builtin/*", []string{"tool:invoke"}, []string{"builtin/*"}, "allow", false},
		{"tool:invoke/mcp/filesystem/*:require_confirmation", []string{"tool:invoke"}, []string{"mcp/filesystem/*"}, "require_confirmation", false},
		{"tool:invoke/daemon/homeserver/*:deny", []string{"tool:invoke"}, []string{"daemon/homeserver/*"}, "deny", false},
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
			"tool:invoke", "builtin/webfetch", true,
		},
		{
			"prefix glob matches",
			Statement{Actions: []string{"tool:invoke"}, Resources: []string{"builtin/*"}, Effect: "allow"},
			"tool:invoke", "builtin/webfetch", true,
		},
		{
			"prefix glob no match",
			Statement{Actions: []string{"tool:invoke"}, Resources: []string{"builtin/*"}, Effect: "allow"},
			"tool:invoke", "mcp/filesystem/read_file", false,
		},
		{
			"action mismatch",
			Statement{Actions: []string{"room:create"}, Resources: []string{"**"}, Effect: "allow"},
			"tool:invoke", "builtin/webfetch", false,
		},
		{
			"exact resource match",
			Statement{Actions: []string{"tool:invoke"}, Resources: []string{"builtin/webfetch"}, Effect: "allow"},
			"tool:invoke", "builtin/webfetch", true,
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
