// Package policy is the authorization engine for Hearth. It evaluates an
// action against a resource for a subject, combining structural clearance
// rules (Bell-LaPadula) with IAM-style permission statements, and returns a
// Decision the caller enforces.
//
// The engine is deliberately decoupled from the agent/inference lifecycle so
// it can be unit-tested in isolation and reused by every access-control call
// site (tool invocation today; data-level actions, declassification, and
// resource policies in future slices).
package policy

// Effect is the enforceable outcome of an authorization decision.
type Effect string

const (
	// Allow permits the action.
	Allow Effect = "allow"
	// Deny blocks the action.
	Deny Effect = "deny"
	// Confirm requires explicit user approval before the action proceeds.
	Confirm Effect = "require_confirmation"
)

// Reason explains why a Decision was reached. It is surfaced for audit logging
// and for differentiated confirmation UI (a permission approval reads
// differently from a clearance write-down redaction). Reason carries no
// enforcement meaning on its own — callers key off Effect.
type Reason string

const (
	// ReasonAllowed: a permission statement allowed the action.
	ReasonAllowed Reason = "allowed"
	// ReasonDefaultDeny: no statement matched (implicit deny).
	ReasonDefaultDeny Reason = "default_deny"
	// ReasonExplicitDeny: a permission statement explicitly denied the action.
	ReasonExplicitDeny Reason = "explicit_deny"
	// ReasonPermConfirm: a permission statement required confirmation.
	ReasonPermConfirm Reason = "permission_confirmation"
	// ReasonReadUp: denied because the subject's clearance is below the
	// resource's (Bell-LaPadula "no read up").
	ReasonReadUp Reason = "blp_read_up"
	// ReasonWriteDown: confirmation required because the subject's clearance is
	// above the resource's, risking a write-down of classified data.
	ReasonWriteDown Reason = "blp_write_down"
)

// Decision is the result of evaluating an authorization Query.
type Decision struct {
	Effect Effect
	Reason Reason
}
