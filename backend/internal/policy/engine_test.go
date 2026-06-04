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
			name:       "BLP write-down on a granted action requires confirmation",
			statements: []config.Statement{allow("**")},
			query:      Query{Action: "tool:invoke", Resource: "x", SubjectClearance: 4, ResourceClearance: 2},
			wantEffect: Confirm,
			wantReason: ReasonWriteDown,
		},
		{
			name:       "BLP write-down without a grant is denied",
			query:      Query{Action: "tool:invoke", Resource: "x", SubjectClearance: 4, ResourceClearance: 2},
			wantEffect: Deny,
			wantReason: ReasonDefaultDeny,
		},
		{
			name:       "explicit deny beats BLP write-down confirm",
			statements: []config.Statement{deny("**")},
			query:      Query{Action: "tool:invoke", Resource: "x", SubjectClearance: 4, ResourceClearance: 2},
			wantEffect: Deny,
			wantReason: ReasonExplicitDeny,
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
			name:       "granted resource clearance zero below subject is write-down",
			statements: []config.Statement{allow("**")},
			query:      Query{Action: "tool:invoke", Resource: "x", SubjectClearance: 3, ResourceClearance: 0},
			wantEffect: Confirm,
			wantReason: ReasonWriteDown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewEngine(tt.statements, nil)
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
			e := NewEngine(tt.statements, nil)
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

func TestDecision_MoreRestrictive(t *testing.T) {
	a := Decision{Effect: Allow, Reason: ReasonAllowed}
	c := Decision{Effect: Confirm, Reason: ReasonWriteDown}
	d := Decision{Effect: Deny, Reason: ReasonExplicitDeny}

	tests := []struct {
		name  string
		left  Decision
		right Decision
		want  Decision
	}{
		{"deny beats confirm, keeps deny reason", d, c, d},
		{"deny beats allow", a, d, d},
		{"confirm beats allow, keeps confirm reason", a, c, c},
		{"allow vs allow keeps receiver", a, a, a},
		{"tie keeps receiver", c, Decision{Effect: Confirm, Reason: ReasonPermConfirm}, c},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.left.MoreRestrictive(tt.right)
			if got != tt.want {
				t.Errorf("MoreRestrictive = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestStatementSet_Defaults(t *testing.T) {
	q := Query{Action: "tool:invoke", Resource: "builtin/x"}

	// Grants default to deny when nothing matches; a matching allow overrides it.
	if got := Grants(nil).Evaluate(q); got.Effect != Deny {
		t.Errorf("empty Grants Effect = %q, want deny", got.Effect)
	}
	if got := Grants([]config.Statement{allow("builtin/*")}).Evaluate(q); got.Effect != Allow {
		t.Errorf("granted Effect = %q, want allow", got.Effect)
	}

	// Restrictions default to allow (abstain) when nothing matches, but can deny.
	if got := Restrictions(nil).Evaluate(q); got.Effect != Allow {
		t.Errorf("empty Restrictions Effect = %q, want allow", got.Effect)
	}
	if got := Restrictions([]config.Statement{deny("builtin/*")}).Evaluate(q); got.Effect != Deny {
		t.Errorf("restricted Effect = %q, want deny", got.Effect)
	}
}

func TestClearanceRule(t *testing.T) {
	r := ClearanceRule{}
	tests := []struct {
		name              string
		subject, resource int
		want              Effect
	}{
		{"zero subject skips check", 0, 5, Allow},
		{"equal abstains", 3, 3, Allow},
		{"read-up denies", 3, 5, Deny},
		{"write-down confirms", 5, 2, Confirm},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.Evaluate(Query{SubjectClearance: tt.subject, ResourceClearance: tt.resource})
			if got.Effect != tt.want {
				t.Errorf("Effect = %q, want %q", got.Effect, tt.want)
			}
		})
	}
}

func TestMostRestrictive_ResourceCannotGrant(t *testing.T) {
	// A resource policy that allows must not rescue an action the agent was
	// never granted: grants default-deny, the resource abstains-or-allows, and
	// the most-restrictive fold keeps the deny.
	q := Query{Action: "tool:invoke", Resource: "builtin/x"}
	tree := MostRestrictive{
		Grants(nil),
		Restrictions([]config.Statement{allow("builtin/*")}),
		ClearanceRule{},
	}
	if got := tree.Evaluate(q); got.Effect != Deny {
		t.Errorf("Effect = %q, want deny (resource cannot grant)", got.Effect)
	}

	// A resource policy that denies overrides an agent grant.
	tree = MostRestrictive{
		Grants([]config.Statement{allow("builtin/*")}),
		Restrictions([]config.Statement{deny("builtin/*")}),
		ClearanceRule{},
	}
	if got := tree.Evaluate(q); got.Effect != Deny {
		t.Errorf("Effect = %q, want deny (resource restricts)", got.Effect)
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
