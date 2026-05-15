package config

import (
	"testing"
)

func TestParsePermission(t *testing.T) {
	tests := []struct {
		raw        string
		wantAction string
		wantScope  string
		wantEffect string
		wantErr    bool
	}{
		{"room:create", "room:create", "", "allow", false},
		{"tool:invoke[email:*]", "tool:invoke", "email:*", "allow", false},
		{"config:write[self]:require_confirmation", "config:write", "self", "require_confirmation", false},
		{"config:read[server]:require_confirmation", "config:read", "server", "require_confirmation", false},
		{"config:read[agent:*]:deny", "config:read", "agent:*", "deny", false},
		{"proactive:act[low, medium]", "proactive:act", "low, medium", "allow", false},
		{"", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			p, err := ParsePermission(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParsePermission(%q) expected error, got nil", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePermission(%q) unexpected error: %v", tt.raw, err)
			}
			if p.Action != tt.wantAction {
				t.Errorf("Action = %q, want %q", p.Action, tt.wantAction)
			}
			if p.Scope != tt.wantScope {
				t.Errorf("Scope = %q, want %q", p.Scope, tt.wantScope)
			}
			if p.Effect != tt.wantEffect {
				t.Errorf("Effect = %q, want %q", p.Effect, tt.wantEffect)
			}
		})
	}
}

func TestAgentDefinition_ParsedPermissions(t *testing.T) {
	agent := &AgentDefinition{
		Name: "test",
		RawPermissions: []string{
			"room:create",
			"tool:invoke[*]",
			"config:write[self]:require_confirmation",
		},
	}

	perms, err := agent.ParsedPermissions()
	if err != nil {
		t.Fatalf("ParsedPermissions returned error: %v", err)
	}

	if len(perms) != 3 {
		t.Fatalf("len(perms) = %d, want 3", len(perms))
	}

	if perms[0].Action != "room:create" || perms[0].Scope != "" || perms[0].Effect != "allow" {
		t.Errorf("perm[0] = %+v, unexpected", perms[0])
	}
	if perms[1].Action != "tool:invoke" || perms[1].Scope != "*" || perms[1].Effect != "allow" {
		t.Errorf("perm[1] = %+v, unexpected", perms[1])
	}
	if perms[2].Action != "config:write" || perms[2].Scope != "self" || perms[2].Effect != "require_confirmation" {
		t.Errorf("perm[2] = %+v, unexpected", perms[2])
	}
}
