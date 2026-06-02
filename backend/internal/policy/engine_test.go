package policy

import (
	"testing"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/config"
)

func allow(resources ...string) config.Statement {
	return config.Statement{Actions: []string{"tool:invoke"}, Resources: resources, Effect: "allow"}
}
func deny(resources ...string) config.Statement {
	return config.Statement{Actions: []string{"tool:invoke"}, Resources: resources, Effect: "deny"}
}
func confirm(resources ...string) config.Statement {
	return config.Statement{Actions: []string{"tool:invoke"}, Resources: resources, Effect: "require_confirmation"}
}

func TestEngine_Evaluate(t *testing.T) {
	tests := []struct {
		name       string
		statements []config.Statement
		query      Query
		wantEffect Effect
		wantReason Reason
	}{
		{
			name:       "no statements is default deny",
			query:      Query{Action: "tool:invoke", Resource: "builtin/x"},
			wantEffect: Deny,
			wantReason: ReasonDefaultDeny,
		},
		{
			name:       "matching allow",
			statements: []config.Statement{allow("builtin/*")},
			query:      Query{Action: "tool:invoke", Resource: "builtin/x"},
			wantEffect: Allow,
			wantReason: ReasonAllowed,
		},
		{
			name:       "wildcard allow",
			statements: []config.Statement{allow("**")},
			query:      Query{Action: "tool:invoke", Resource: "builtin/calendar_read"},
			wantEffect: Allow,
			wantReason: ReasonAllowed,
		},
		{
			name:       "non-matching resource is default deny",
			statements: []config.Statement{allow("daemon/*")},
			query:      Query{Action: "tool:invoke", Resource: "builtin/x"},
			wantEffect: Deny,
			wantReason: ReasonDefaultDeny,
		},
		{
			name:       "explicit deny",
			statements: []config.Statement{deny("builtin/*")},
			query:      Query{Action: "tool:invoke", Resource: "builtin/x"},
			wantEffect: Deny,
			wantReason: ReasonExplicitDeny,
		},
		{
			name:       "deny beats allow regardless of order",
			statements: []config.Statement{allow("builtin/*"), deny("builtin/*")},
			query:      Query{Action: "tool:invoke", Resource: "builtin/x"},
			wantEffect: Deny,
			wantReason: ReasonExplicitDeny,
		},
		{
			name:       "confirm beats allow",
			statements: []config.Statement{allow("builtin/*"), confirm("builtin/*")},
			query:      Query{Action: "tool:invoke", Resource: "builtin/x"},
			wantEffect: Confirm,
			wantReason: ReasonPermConfirm,
		},
		{
			name:       "deny beats confirm",
			statements: []config.Statement{confirm("builtin/*"), deny("builtin/*")},
			query:      Query{Action: "tool:invoke", Resource: "builtin/x"},
			wantEffect: Deny,
			wantReason: ReasonExplicitDeny,
		},
		{
			name:       "action mismatch is default deny",
			statements: []config.Statement{allow("builtin/*")},
			query:      Query{Action: "data:read", Resource: "builtin/x"},
			wantEffect: Deny,
			wantReason: ReasonDefaultDeny,
		},
		// --- BLP ---
		{
			name:       "BLP read-up denies, non-overridable by allow",
			statements: []config.Statement{allow("**")},
			query:      Query{Action: "tool:invoke", Resource: "x", SubjectClearance: 3, ResourceClearance: 5},
			wantEffect: Deny,
			wantReason: ReasonReadUp,
		},
		{
			name:       "BLP write-down requires confirmation",
			query:      Query{Action: "tool:invoke", Resource: "x", SubjectClearance: 4, ResourceClearance: 2},
			wantEffect: Confirm,
			wantReason: ReasonWriteDown,
		},
		{
			name:       "BLP equal clearance falls through to allow",
			statements: []config.Statement{allow("builtin/*")},
			query:      Query{Action: "tool:invoke", Resource: "builtin/x", SubjectClearance: 3, ResourceClearance: 3},
			wantEffect: Allow,
			wantReason: ReasonAllowed,
		},
		{
			name:       "BLP equal clearance with no permission is default deny",
			query:      Query{Action: "tool:invoke", Resource: "builtin/x", SubjectClearance: 3, ResourceClearance: 3},
			wantEffect: Deny,
			wantReason: ReasonDefaultDeny,
		},
		{
			name:       "SubjectClearance zero skips BLP (resource clearance ignored)",
			statements: []config.Statement{allow("builtin/*")},
			query:      Query{Action: "tool:invoke", Resource: "builtin/x", SubjectClearance: 0, ResourceClearance: 5},
			wantEffect: Allow,
			wantReason: ReasonAllowed,
		},
		{
			name:       "resource clearance zero below subject is write-down",
			query:      Query{Action: "tool:invoke", Resource: "x", SubjectClearance: 3, ResourceClearance: 0},
			wantEffect: Confirm,
			wantReason: ReasonWriteDown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewEngine(tt.statements)
			got := e.Evaluate(tt.query)
			if got.Effect != tt.wantEffect {
				t.Errorf("Effect = %q, want %q", got.Effect, tt.wantEffect)
			}
			if got.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

func TestEngine_EvaluateAll(t *testing.T) {
	// data:read statement helpers for two-tier cases.
	dataRead := func(effect string, resources ...string) config.Statement {
		return config.Statement{Actions: []string{"data:read"}, Resources: resources, Effect: effect}
	}

	tests := []struct {
		name       string
		statements []config.Statement
		queries    []Query
		wantEffect Effect
		wantReason Reason
	}{
		{
			name:       "no queries is default deny",
			wantEffect: Deny,
			wantReason: ReasonDefaultDeny,
		},
		{
			name:       "single capability allow",
			statements: []config.Statement{allow("builtin/*")},
			queries:    []Query{{Action: "tool:invoke", Resource: "builtin/x"}},
			wantEffect: Allow,
			wantReason: ReasonAllowed,
		},
		{
			name:       "both tiers allow",
			statements: []config.Statement{allow("builtin/*"), dataRead("allow", "builtin/*")},
			queries: []Query{
				{Action: "tool:invoke", Resource: "builtin/x"},
				{Action: "data:read", Resource: "builtin/x"},
			},
			wantEffect: Allow,
			wantReason: ReasonAllowed,
		},
		{
			name:       "capability allows but data tier default-denies",
			statements: []config.Statement{allow("builtin/*")},
			queries: []Query{
				{Action: "tool:invoke", Resource: "builtin/x"},
				{Action: "data:read", Resource: "builtin/x"},
			},
			wantEffect: Deny,
			wantReason: ReasonDefaultDeny,
		},
		{
			name:       "data tier explicit deny beats capability allow",
			statements: []config.Statement{allow("builtin/*"), dataRead("deny", "builtin/*")},
			queries: []Query{
				{Action: "tool:invoke", Resource: "builtin/x"},
				{Action: "data:read", Resource: "builtin/x"},
			},
			wantEffect: Deny,
			wantReason: ReasonExplicitDeny,
		},
		{
			name:       "data tier confirm escalates from allow",
			statements: []config.Statement{allow("builtin/*"), dataRead("require_confirmation", "builtin/*")},
			queries: []Query{
				{Action: "tool:invoke", Resource: "builtin/x"},
				{Action: "data:read", Resource: "builtin/x"},
			},
			wantEffect: Confirm,
			wantReason: ReasonPermConfirm,
		},
		{
			name:       "most restrictive wins regardless of query order",
			statements: []config.Statement{deny("builtin/*"), dataRead("allow", "builtin/*")},
			queries: []Query{
				{Action: "data:read", Resource: "builtin/x"},
				{Action: "tool:invoke", Resource: "builtin/x"},
			},
			wantEffect: Deny,
			wantReason: ReasonExplicitDeny,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewEngine(tt.statements)
			got := e.EvaluateAll(tt.queries...)
			if got.Effect != tt.wantEffect {
				t.Errorf("Effect = %q, want %q", got.Effect, tt.wantEffect)
			}
			if got.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

func TestClearanceCheck(t *testing.T) {
	tests := []struct {
		name              string
		subject, resource int
		wantOK            bool
		wantEffect        Effect
		wantReason        Reason
	}{
		{"read-up fires", 3, 5, true, Deny, ReasonReadUp},
		{"write-down fires", 4, 2, true, Confirm, ReasonWriteDown},
		{"equal does not fire", 3, 3, false, "", ""},
		{"resource zero below subject is write-down", 3, 0, true, Confirm, ReasonWriteDown},
		{"subject zero with positive resource is read-up", 0, 5, true, Deny, ReasonReadUp},
		{"both zero does not fire", 0, 0, false, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, ok := ClearanceCheck(tt.subject, tt.resource)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if d.Effect != tt.wantEffect {
				t.Errorf("Effect = %q, want %q", d.Effect, tt.wantEffect)
			}
			if d.Reason != tt.wantReason {
				t.Errorf("Reason = %q, want %q", d.Reason, tt.wantReason)
			}
		})
	}
}
