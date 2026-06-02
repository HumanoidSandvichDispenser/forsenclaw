package policy

import "github.com/humanoidsandvichdispenser/hearth/backend/internal/config"

// Query is a single authorization question: may the subject perform Action on
// Resource, given the clearance context?
type Query struct {
	// Action is the verb being authorized, e.g. "tool:invoke".
	Action string
	// Resource is the FRSN of the target, e.g. "frsn:tool/builtin/webfetch".
	Resource string
	// SubjectClearance is the effective clearance of the acting subject.
	// When zero, the Bell-LaPadula pre-check is skipped and only permission
	// statements are evaluated (mirrors the inference handler's
	// `effectiveClearance > 0` guard).
	SubjectClearance int
	// ResourceClearance is the data-classification tier of the resource.
	ResourceClearance int
}

// Engine evaluates queries against a fixed set of permission statements
// (typically an agent's permissions).
type Engine struct {
	statements []config.Statement
}

// NewEngine returns an Engine backed by the given permission statements.
func NewEngine(statements []config.Statement) *Engine {
	return &Engine{statements: statements}
}

// ClearanceCheck applies the structural Bell-LaPadula rule and nothing else.
// It is the single source of truth for read-up / write-down enforcement.
//
// The comparison is pure (no subject>0 guard): the caller decides whether the
// check applies. ok is false when the rule does not fire (equal clearance),
// meaning the caller should fall through to permission evaluation.
func ClearanceCheck(subjectClearance, resourceClearance int) (Decision, bool) {
	if resourceClearance > subjectClearance {
		// No read up: subject cannot safely handle data at this classification.
		return Decision{Effect: Deny, Reason: ReasonReadUp}, true
	}
	if resourceClearance < subjectClearance {
		// No write down without approval: subject may carry classified data
		// into a lower-clearance resource.
		return Decision{Effect: Confirm, Reason: ReasonWriteDown}, true
	}
	return Decision{}, false
}

// Evaluate decides a Query. Bell-LaPadula is checked first and is structural —
// it is not overridable by permission statements. If BLP does not fire,
// permission statements are evaluated with precedence
// deny > require_confirmation > allow, defaulting to deny when nothing matches.
func (e *Engine) Evaluate(q Query) Decision {
	if q.SubjectClearance > 0 {
		if d, ok := ClearanceCheck(q.SubjectClearance, q.ResourceClearance); ok {
			return d
		}
	}

	effect := Effect("")
	reason := ReasonDefaultDeny
	for _, stmt := range e.statements {
		if !stmt.Matches(q.Action, q.Resource) {
			continue
		}
		switch stmt.Effect {
		case string(Deny):
			// Explicit deny short-circuits everything.
			return Decision{Effect: Deny, Reason: ReasonExplicitDeny}
		case string(Confirm):
			effect = Confirm
			reason = ReasonPermConfirm
		case string(Allow):
			if effect == "" {
				effect = Allow
				reason = ReasonAllowed
			}
		}
	}
	if effect == "" {
		return Decision{Effect: Deny, Reason: ReasonDefaultDeny}
	}
	return Decision{Effect: effect, Reason: reason}
}
