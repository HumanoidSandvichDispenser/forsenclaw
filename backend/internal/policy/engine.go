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
	// When zero, the Bell-LaPadula check is skipped and only permission
	// statements are evaluated (mirrors the inference handler's
	// `effectiveClearance > 0` guard).
	SubjectClearance int
	// ResourceClearance is the data-classification tier of the resource.
	ResourceClearance int
}

// Evaluator answers a Query with a Decision. Each source of authority (the
// agent's grants, a resource's own policy, the clearance rule) is an Evaluator,
// so they compose uniformly through MostRestrictive.
type Evaluator interface {
	Evaluate(Query) Decision
}

// StatementSet evaluates a set of IAM-style permission statements. The same
// type backs both an agent's grants and a resource's policy; the only
// difference is nomatch — the Decision returned when no statement applies.
//
// Grants default to deny (a subject must be explicitly granted). Resource
// policies default to allow (silence is no objection; a resource may only
// restrict, never grant).
type StatementSet struct {
	stmts   []config.Statement
	nomatch Decision
}

// Grants is the agent-side statement set: nothing matching means deny.
func Grants(stmts []config.Statement) StatementSet {
	return StatementSet{stmts: stmts, nomatch: Decision{Effect: Deny, Reason: ReasonDefaultDeny}}
}

// Restrictions is the resource-side statement set: nothing matching means allow
// (abstain). It can only ever make a decision more restrictive, never grant.
func Restrictions(stmts []config.Statement) StatementSet {
	return StatementSet{stmts: stmts, nomatch: Decision{Effect: Allow, Reason: ReasonAllowed}}
}

// Evaluate resolves the matching statements against each other (most
// restrictive wins, seeded at allow) and falls back to nomatch when none apply.
// The fallback is not folded into the resolution: a matching allow must be able
// to override the deny default, so nomatch only stands in when nothing matched.
func (s StatementSet) Evaluate(q Query) Decision {
	matched := false
	result := Decision{Effect: Allow, Reason: ReasonAllowed}
	for _, stmt := range s.stmts {
		if !stmt.Matches(q.Action, q.Resource) {
			continue
		}
		matched = true
		result = result.MoreRestrictive(decisionFor(stmt))
	}
	if !matched {
		return s.nomatch
	}
	return result
}

// decisionFor maps a statement's effect to a Decision with the matching reason.
func decisionFor(stmt config.Statement) Decision {
	switch stmt.Effect {
	case string(Deny):
		return Decision{Effect: Deny, Reason: ReasonExplicitDeny}
	case string(Confirm):
		return Decision{Effect: Confirm, Reason: ReasonPermConfirm}
	default:
		return Decision{Effect: Allow, Reason: ReasonAllowed}
	}
}

// ClearanceRule applies the structural Bell-LaPadula check. It is its own
// Evaluator because it compares clearances rather than matching statements;
// it can only deny (read-up) or require confirmation (write-down), never grant.
type ClearanceRule struct{}

// Evaluate fires BLP when the subject carries a clearance. A zero subject
// clearance skips the check, and equal clearance abstains (returns allow).
func (ClearanceRule) Evaluate(q Query) Decision {
	if q.SubjectClearance <= 0 {
		return Decision{Effect: Allow, Reason: ReasonAllowed}
	}
	if d, ok := ClearanceCheck(q.SubjectClearance, q.ResourceClearance); ok {
		return d
	}
	return Decision{Effect: Allow, Reason: ReasonAllowed}
}

// MostRestrictive evaluates its children and folds their decisions with
// Decision.MoreRestrictive. Seeded at allow (the identity), an empty set or a
// set of all-abstaining sources yields allow; any objection raises the result.
type MostRestrictive []Evaluator

// Evaluate folds the children's decisions, most restrictive winning.
func (m MostRestrictive) Evaluate(q Query) Decision {
	result := Decision{Effect: Allow, Reason: ReasonAllowed}
	for _, e := range m {
		result = result.MoreRestrictive(e.Evaluate(q))
	}
	return result
}

// ClearanceCheck applies the structural Bell-LaPadula rule and nothing else.
// It is the single source of truth for read-up / write-down enforcement, used
// both by ClearanceRule and by the tool-filtering pass in the inference handler.
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

// severity ranks effects from least to most restrictive, so MoreRestrictive can
// pick the harder-fail of two decisions.
func severity(e Effect) int {
	switch e {
	case Deny:
		return 2
	case Confirm:
		return 1
	default: // Allow
		return 0
	}
}

// Engine evaluates queries against a fixed set of permission statements
// (typically an agent's grants).
type Engine struct {
	grants []config.Statement
}

// NewEngine returns an Engine backed by the given grant statements.
func NewEngine(grants []config.Statement) *Engine {
	return &Engine{grants: grants}
}

// EvaluateAll decides a set of queries that must ALL pass for an action to
// proceed, returning the single most restrictive Decision. This is the
// two-tier authorization primitive: a tool call is gated by its capability
// action (tool:invoke) AND any data-level actions it performs (data:read /
// data:write), each evaluated through the same sources.
//
// With no queries it returns a default deny — callers always pass at least the
// capability query.
func (e *Engine) EvaluateAll(queries ...Query) Decision {
	if len(queries) == 0 {
		return Decision{Effect: Deny, Reason: ReasonDefaultDeny}
	}
	result := Decision{Effect: Allow, Reason: ReasonAllowed}
	for _, q := range queries {
		result = result.MoreRestrictive(e.Evaluate(q))
	}
	return result
}

// Evaluate decides a Query by combining every source of authority and taking
// the most restrictive outcome: the clearance rule, the agent's grants (which
// must allow), and — once wired — a resource's own policy. Grants default to
// deny, so an action no source grants is denied; the clearance rule and
// resource policy can only tighten that, never loosen it.
//
// The clearance rule is folded first so its specific reason (read-up /
// write-down) survives a tie against the generic default-deny: when an action
// is both a read-up and ungranted, the structural reason is the useful one. A
// strictly more severe statement decision still wins on effect as usual.
func (e *Engine) Evaluate(q Query) Decision {
	return MostRestrictive{
		ClearanceRule{},
		Grants(e.grants),
	}.Evaluate(q)
}
