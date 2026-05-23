= Frontend <frontend>

forsenClaw ships a Vue 3 + TypeScript + Pinia web frontend served by the Go
binary. The frontend communicates with the backend via WebSocket for real-time
updates and REST for state queries.

== Views

/ Room list: All rooms the user participates in. Shows unread count and
  notification badges. Grouped by project if applicable.
/ Room view: The active transcript, message composer, participant list, and
  clearance ceiling control. Streaming agent responses rendered in real time.
/ Agent config viewer: Read-only view of an agent's `agent.yaml`. Diff view
  for staged config proposals awaiting approval.
/ Settings: Server config (`hearth.yaml`), model provider registry, clearance
  level label customization.
/ Audit log viewer: Filterable list of audit events. DAG visualization for
  individual Requests.

== Clearance Ceiling Filter

A global clearance ceiling filter allows the user to set a session-wide ceiling
(e.g., clearance 2 at work). When active:

- Messages tagged above the ceiling are hidden from the transcript view.
- Memory views show only strata ≤ the ceiling.
- The active room's clearance is constrained to ≤ the ceiling.
- Agent context assembly respects the lower ceiling for the duration of the
  session.

The ceiling filter is a session UI setting --- it does not mutate stored data.
Dropping the filter restores full visibility.

== Room Clearance Shift

Each room view exposes a clearance ceiling control. The user can shift the
room's clearance up or down at any time. This is the primary mechanism for
"external-safe mode" when composing content destined for external integrations.

Shifting down is a deliberate, explicit boundary crossing. The intent is that
the act of shifting is the approval --- not a per-sentence confirm dialog.

The current clearance level is always visible in the room UI so the user knows
what context the agent has available.

== Request DAG Visualization

The audit log viewer includes a DAG visualization for individual Requests. Each
node shows:

- Request ID, source (room/system/event), target agent.
- Current state (pending / in_progress / blocked / resolved / failed).
- Blocked-by edges to child Requests or approval nodes.
- Timestamps and resolution outcomes.

This provides a full proof trace for debugging agent behavior, reviewing
delegations, and auditing tool use.

== Config Proposal Diff View

When an agent proposes a config change, the frontend surfaces a diff review
panel showing the current file and proposed file side-by-side. The user can:

- Approve the proposal as-is.
- Edit the proposed file inline before approving.
- Reject the proposal.

The same panel is used for OPA policy proposals.
