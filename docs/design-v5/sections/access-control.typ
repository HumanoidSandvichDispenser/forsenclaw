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
only their relative ordering matters, not their magnitude or consecutiveness.
A scheme of `[1, 4, 5, 20]` works identically to `[1, 2, 3, 4]`. Higher numbers
mean more trust and more sensitive data. The number of tiers and their
descriptions are fully user-configurable in `hearth.yaml`.

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
semantics; the integers carry the ordering. Levels need not be consecutive.

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

Tool schemas injected into the context are also annotated with their clearance
level (see @mcp). This makes data boundaries at the tool surface explicit
without hiding available tools.

=== Room Clearance Shifting

The user can shift a room's clearance up or down at any time. Shifting down
is a deliberate "external-safe mode" for composing outbound content. The shift
is the intentional boundary crossing --- not a per-sentence approval. The
frontend exposes a global clearance ceiling filter for session-wide scoping
(see @frontend).

=== Bell-LaPadula Enforcement

BLP rules are enforced at five points:

+ *Context assembly* --- only MEMORY-k.md and daily note files where k ≤ room
  clearance are included. Retrieval queries from the search index are filtered
  to the same ceiling.
+ *Outgoing message send* --- the dispatcher checks a message's clearance tag
  against the destination room's ceiling. Rejected if above.
+ *Spawn-time context injection* --- context passed to a child agent must not
  exceed the child's clearance. Dispatcher rejects violating spawns.
+ *Tool injection* --- only tool schemas whose clearance ≤ room ceiling are
  injected into the agent's assembled context. Tools above the ceiling are
  structurally absent --- the agent cannot request data from them.
+ *Tool invocation* --- tools below the room ceiling require confirmation when
  invoked from a higher-clearance room. The agent may see and attempt to call
  them, but any outbound data flow through a lower-clearance tool is a
  potential write-down.

No read-up: agents cannot read data above their current clearance level.

No write-down: agents cannot write data below their current clearance level
without explicit user approval (redaction proposal flow). See @memory for
details.

Classification at write time: a message sent in a clearance-3 room is tagged
clearance-3 in the transcript and never appears in a clearance-2 retrieval.
Default classification is the sender's clearance, capped at the room's ceiling.

=== Tool Clearance

MCP tools are assigned a clearance level in `hearth.yaml` (see @storage). Tool
clearance is a *data classification*, not a permission --- it answers the
question "what level of data does this tool access or emit?" rather than "is
the agent allowed to use this tool?"

Tool clearance and agent permissions are orthogonal: an agent must hold
`tool:invoke[<tool_id>]` *and* satisfy BLP rules for the invocation to proceed.
Permission governs capability; clearance governs data boundaries. The two
checks happen at different enforcement points --- permissions at dispatch, BLP
at assembly and invocation. BLP write-down confirmation is structural and cannot
be overridden by an `allow` permission; it uses the same `require_confirmation`
flow as permission-gated confirmations, but originates from a different source
and cannot be bypassed.

#table(
  columns: (auto, auto, 1fr),
  table.header([*Condition*], [*Result*], [*Rationale*]),
  [Tool clearance > room ceiling],
  [Not injected, not callable],
  [No read-up: a higher-clearance tool returns data above the agent's assembled
  context. The tool is structurally absent, same as MEMORY-k.md where k >
  ceiling.],
  [Tool clearance = room ceiling],
  [Injected, callable (subject to permissions)],
  [Same level. No BLP conflict.],
  [Tool clearance < room ceiling],
  [Injected, invocation requires confirmation],
  [No write-down without approval: calling a lower-clearance tool from a
  higher-clearance room is a potential data leak. Read-down is permitted (the
  agent can see the tool exists and request to use it), but invocation carries
  write-down risk.],
)

The default tool clearance, if not specified, is the system maximum (the highest
level defined in `clearance_levels`). This is conservative: unlabeled tools are
assumed to handle the most sensitive data and are only available at the highest
clearance. Tools that handle public or external-safe data must be explicitly
labeled.

Tools below the room ceiling are annotated with their clearance level when
injected, making data boundaries explicit:

```
[Tool: email_send — clearance 2 (external) — requires confirmation
in this clearance-4 room due to write-down risk. Minimize or redact
sensitive content before invoking, or use propose_handoff for deliberate
content transfer.]
```

This mirrors soft Biba integrity tagging: the agent can see the tool and
understand its data boundary, but invocation is gated by a confirmation step
rather than silently permitted. The confirmation request surfaces the tool call
arguments to the user for review, consistent with the existing
`require_confirmation` DAG flow (see @rooms).

When multiple write-down operations are needed, the agent should spawn a
lower-clearance ephemeral (see @agents) rather than confirming each call
individually. An ephemeral agent operating at the tool's clearance level has no
write-down risk and can invoke the tool freely within its own ceiling.

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
forsen passing a redacted email summary to a clearance-2 integration agent:

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

Permissions are fine-grained action capabilities defined as IAM-style
statements in each agent's `agent.yaml`. Each statement grants or restricts an
action over a resource path. The system evaluates all matching statements with
*deny → require_confirmation → allow* precedence; default deny applies when no
statement matches.

*Current implementation:* YAML statements are evaluated directly by the
dispatcher using glob matching.

*Intended direction:* YAML statements are syntactic sugar that compile to Rego
facts. OPA evaluates the full policy, allowing agents to also supply raw Rego
for complex conditions that statements cannot express (time-of-day, argument
inspection, cross-attribute rules). The compiled form of a YAML statement like:

```yaml
- tool:invoke/mcp/email/send:require_confirmation
```

would produce Rego facts equivalent to:

```rego
allow {
  input.subject == "forsen"
  input.action == "tool:invoke"
  input.resource == "mcp/email/send"
}

require_confirmation {
  input.subject == "forsen"
  input.action == "tool:invoke"
  input.resource == "mcp/email/send"
}
```

Both `allow` and `require_confirmation` must hold for confirmation to trigger;
`allow` alone means execute freely, `require_confirmation` alone is unreachable
(default deny blocks it).

Agents that need richer logic write Rego directly. Both paths feed the same
OPA evaluation.

=== Statement Format

Statements may be written in shorthand string form or as structured mappings:

```yaml
permissions:
  # shorthand: action/resource[:effect]
  - tool:invoke/frsn:tool/builtin/webfetch
  - tool:invoke/frsn:tool/mcp/email/*:require_confirmation
  - tool:invoke/frsn:tool/mcp/finances/**:deny

  # structured form — multiple actions or resources in one statement
  - effect: require_confirmation
    actions: [tool:invoke]
    resources: [mcp/calendar/*, mcp/email/*]
```

Resource paths use glob patterns (`path.Match` semantics). `**` matches any
resource path.

=== Permission Sets

Common grants can be defined once as a named set in `hearth.yaml` and referenced
by name, so the same statements need not be copied across agents:

```yaml
# hearth.yaml
permission_sets:
  web-tools:
    - tool:invoke/frsn:tool/builtin/webfetch
    - tool:invoke/frsn:tool/builtin/web_search
  untrusted:
    - tool:invoke/frsn:tool/builtin/create_room:deny
```

```yaml
# agent.yaml
permission_sets: [web-tools, untrusted]
permissions:
  - tool:invoke/frsn:tool/builtin/calendar_read
```

A referenced set's statements are merged into the agent's grants at load and are
grants like any other: an allow grants, a deny vetoes. Because grants resolve
most restrictive, a deny in any set wins over an allow elsewhere. Sets are flat
--- a set holds statements, not references to other sets --- so there is no
inheritance chain to resolve.

=== Evaluation Order

+ *deny* --- unconditional, short-circuits immediately. No specificity
  ambiguity.
+ *require_confirmation* --- wins over `allow` when both apply.
+ *allow* --- explicit grant.
+ *default deny* --- applies when no statement matches.

=== Sources and Resolution

Authorization draws on more than the agent's own statements. Each source of
authority is evaluated independently and the most restrictive outcome wins
(`deny > require_confirmation > allow`):

- *Agent grants* --- the agent's `permissions`. This is the only source that can
  grant; with no matching grant the result defaults to deny.
- *Resource policies* --- statements scoped to the resource rather than the
  agent (`resource_policies` in `hearth.yaml`). They can only restrict: a
  matching deny or require_confirmation tightens a call, but a resource never
  grants access the agent's own permissions lack. With no match a resource
  policy abstains.
- *Clearance (BLP)* --- the structural read-up / write-down rule. It can only
  deny or require confirmation, and an `allow` cannot override it.

A resource policy lets a sensitive tool protect itself once, at the resource,
rather than depending on every agent definition being written correctly. Because
the agent is the only grant source, the capabilities an agent holds are still
answered by reading that one agent's `permissions`; resource policies and
clearance only subtract from it.

=== Permission Categories

- `tool:invoke/<server>/<tool>` --- MCP tool dispatch. Server is `builtin` for
  built-in tools, or the configured server name for remote MCP servers.
- `room:create`, `room:add_participant`, `room:close`,
  `room:extend_turn_limit`.
- `memory:write/<layer>`, `memory:search/<scope>` --- write to own memory
  files, search across agents.
- `agent:spawn/<role>`, `agent:terminate`, `agent:compact/<target>`.
- `config:read/<scope>`, `config:write/<scope>` --- read/write agent or
  server configuration files. Scopes: `self`, `server`, `agent/<name>`.
- `proactive:enable`, `proactive:act/<risk_tier>`.
- `handoff:propose/<target_clearance>`.
- `audit:read`.

=== Default Effects for Config Permissions

#table(
  columns: (auto, auto),
  table.header([*Permission*], [*Default Effect*]),
  [`config:read/self`], [`allow`],
  [`config:write/self`], [`require_confirmation`],
  [`config:read/server`], [`require_confirmation`],
  [`config:write/server`], [`require_confirmation`],
  [`config:read/agent/*`], [`deny`],
  [`config:write/agent/*`], [`deny`],
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
