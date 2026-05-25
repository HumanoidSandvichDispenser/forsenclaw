= Access Control <access-control>

Access control has two orthogonal axes with distinct enforcement points and
mechanisms.

*Clearance* governs what an actor can *see* (data visibility). Enforced via
Bell-LaPadula (BLP) rules applied during context assembly and retrieval.

*Permissions* govern what an actor can *do* (action capability). Enforced via
Attribute-Based Access Control (ABAC) implemented with embedded OPA (Open
Policy Agent) and Rego policies.

These axes are orthogonal. Operating at low clearance does not grant
low-clearance tool access; permissions are evaluated independently.

== Clearance --- Context Scope

Clearance is a tiered data classification. Levels are totally ordered integers;
higher numbers mean more trust and more sensitive data. The number of tiers and
their descriptions are fully user-configurable in `hearth.yaml`.

*Clearance is not merely an access filter --- it is a context scope.* The
room's clearance level determines which memory strata the agent assembles.
An agent operating in a clearance-2 room has only `MEMORY-1.md` and
`MEMORY-2.md` in context. It cannot leak clearance-4 data because that data is
not present. This is a structural DLP guarantee.

=== Default Five-Level Scheme

#table(
  columns: (auto, auto, 1fr),
  table.header([*Level*], [*Label*], [*Description*]),
  [1], [public],
  [Safe to expose anywhere. Role, communication style. Nothing personal.],
  [2], [external],
  [Safe for external integrations. Preferences, timezone, things that inform
  external actions without revealing why.],
  [3], [professional],
  [Task-scoped context. Active projects, professional context.],
  [4], [personal],
  [Full personal context. Default landing zone for dreaming output.],
  [5], [private],
  [Vault tier. Health, finances, anything where exposure causes real damage.
  Sparse, manually curated only.],
)

Users can add, remove, renumber, or rename levels. The labels carry the
semantics; the integers carry the ordering.

=== Clearance Notice Injection

Lower-clearance contexts are aware that higher-clearance data exists. The
assembler injects a notice at the top of every context assembly:

```
You are operating at clearance level 2 (external). Higher-clearance
context exists but is not available in this context. If a question
requires deeper personal context, say so rather than guessing.
```

This prevents the agent from fabricating personal details it does not have
access to.

=== Room Clearance Shifting

The user can shift a room's clearance up or down at any time. Shifting down
is a deliberate "external-safe mode" for composing outbound content. The shift
is the intentional boundary crossing --- not a per-sentence approval. The
frontend exposes a global clearance ceiling filter for session-wide scoping
(see @frontend).

=== Bell-LaPadula Enforcement

BLP rules are enforced at three points:

+ *Context assembly* --- only MEMORY-k.md and daily note files where k ≤ room
  clearance are included. Retrieval queries from the search index are filtered
  to the same ceiling.
+ *Outgoing message send* --- the dispatcher checks a message's clearance tag
  against the destination room's ceiling. Rejected if above.
+ *Spawn-time context injection* --- context passed to a child agent must not
  exceed the child's clearance. Dispatcher rejects violating spawns.

No read-up: agents cannot read data above their current clearance level.

No write-down: agents cannot write data below their current clearance level
without explicit user approval (redaction proposal flow). See @memory for
details.

Classification at write time: a message sent in a clearance-3 room is tagged
clearance-3 in the transcript and never appears in a clearance-2 retrieval.
Default classification is the sender's clearance, capped at the room's ceiling.

=== Soft Integrity Tagging (Soft Biba)

True Biba enforcement — no-read-down and no-write-up as hard integrity rules —
is a v2 concern. v1 implements a lightweight integrity signal: when input
arrives from a lower-clearance source in a higher-clearance context, the
assembler annotates it:

```
[Source: clearance-2 (external) — treat with appropriate skepticism]
<content>
```

This makes provenance explicit without blocking the information. The annotation
applies to:

- Messages in the transcript tagged below the current room ceiling.
- Tool results from external sources (clearance determined by the tool's
  declared level).
- Content forwarded from lower-clearance rooms via the cross-room feed.

The agent may act on the content but is aware it came from a less-trusted
source and should not treat it as high-trust personal context.

Full Biba (hard no-read-down, write integrity labels, subject integrity levels)
is deferred to v2.

=== Explicit Cross-Clearance Handoff

When an agent deliberately wants to pass sensitive content down --- e.g., the
housewife passing a redacted email summary to a clearance-2 integration agent:

```
propose_handoff(
    source_content: <high-clearance content>,
    target_clearance: <lower level>,
    destination: <agent or room>,
) -> proposed_redaction
```

The agent generates a proposed redaction using its `sensitive_model`. The
dispatcher shows original and proposed redaction to the user side-by-side. User
reviews, optionally edits, and approves or rejects. Only on approval does the
redacted content cross the boundary.

== Permissions --- What an Actor Can Do

Permissions are fine-grained action capabilities evaluated by embedded OPA
using Rego policies. Policy files live on disk at
`$XDG_CONFIG_HOME/forsenClaw/policy.rego`, are git-trackable, human-editable,
and diffable. Agents can propose policy changes via the config staging mechanic.

=== OPA Evaluation Model

The evaluation order is: *deny → require_confirmation → allow.*

- `deny` is unconditional and wins over all other rules. No specificity
  ambiguity.
- `require_confirmation` wins over `allow` when both apply (more restrictive
  takes precedence).
- Default deny everything not explicitly allowed.

=== Example Policy

```rego
package hearth.authz

default allow = false
default require_confirmation = false

# BLP: no write-down without approval
deny {
  input.action == "memory:write"
  input.target_clearance < input.room.clearance
}

# ABAC: email send allowed during business hours at clearance 2
allow {
  input.action == "tool:invoke"
  input.tool == "email:send"
  input.room.clearance <= 2
  input.agent.permissions[_] == "tool:invoke[email:send]"
  is_business_hours
}

# Require confirmation outside business hours
require_confirmation {
  input.action == "tool:invoke"
  input.tool == "email:send"
  not is_business_hours
}

is_business_hours {
  hour := time.clock(time.now_ns())[0]
  hour >= 9
  hour < 17
}
```

=== Permission Categories

- `tool:invoke[<tool_id>]` --- MCP tool dispatch.
- `room:create`, `room:add_participant`, `room:close`,
  `room:extend_turn_limit`.
- `memory:write[<layer>]`, `memory:search[<scope>]` --- write to own memory
  files, search across agents.
- `agent:spawn[<role>]`, `agent:terminate`, `agent:compact[<target>]`.
- `config:read[<scope>]`, `config:write[<scope>]` --- read/write agent or
  server configuration files. Scopes: `self`, `server`, `agent:<name>`.
- `proactive:enable`, `proactive:act[<risk_tier>]`.
- `handoff:propose[<target_clearance>]`.
- `audit:read`.
- `policy:propose` --- propose changes to the OPA policy file.

=== Default Effects for Config Permissions

#table(
  columns: (auto, auto),
  table.header([*Permission*], [*Default Effect*]),
  [`config:read[self]`], [`allow`],
  [`config:write[self]`], [`require_confirmation`],
  [`config:read[server]`], [`require_confirmation`],
  [`config:write[server]`], [`require_confirmation`],
  [`config:read[agent:*]`], [`deny`],
  [`config:write[agent:*]`], [`deny`],
  [`policy:propose`], [`require_confirmation`],
)

== Config Staging and Approval

When an agent proposes a config change (writing to any file under
`$XDG_CONFIG_HOME/forsenClaw/`), the flow is:

+ Agent writes the proposed new file to the staging area:
  `$XDG_DATA_HOME/forsenClaw/staging/agents/<name>/agent.yaml.proposed`.
+ OPA evaluates the write permission. If `require_confirmation`: the dispatcher
  computes a diff and surfaces it to the user for review. The user approves,
  edits, or rejects.
+ If the effect is `allow`: the system applies the change directly.
+ If the effect is `deny`: the agent cannot propose the change.

On approval, the proposed file replaces the config file. On rejection or
expiry, the proposed file is deleted. The audit log records the proposal, diff,
and outcome.

One pending proposal per agent per config file. A new proposal overwrites the
previous one.

Policy file proposals follow the same staging flow with `policy:propose`.

== Audit

The audit log records *actions taken* and *attempts denied*:

- Tool invocations (call + arguments + result status).
- Permission denials and OPA evaluation results.
- `require_confirmation` checks (request + user response).
- Config proposals (diff + outcome).
- Agent spawn and teardown (ephemeral agent definitions snapshotted here).
- Cross-clearance rejections.
- Handoff proposals and resolutions.
- Request DAG traversal (full proof trace; see @rooms).

Append-only, SQLite-backed, accessible to the user without restriction.
`audit:read` is its own permission for agents; they do not get it by default.
