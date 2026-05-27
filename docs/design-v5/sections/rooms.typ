= Rooms <rooms>

Rooms are clearance-bounded conversation spaces. Their primary role is
information architecture — defining what an agent can see — not orchestration.
A room is a context isolation unit.

== Room Structure

A room has:

- A *participant list* (actors — users and/or agents).
- A *message log* (SQLite table keyed by `(room_id, number)`).
- A *clearance ceiling* — the highest clearance level any message may carry,
  and the scope at which agents assemble context when operating here.
- A *parent* (optional) — for structural grouping under a project.

Room metadata and transcripts are both stored in SQLite.

== Clearance Ceiling as Confidentiality Boundary

The clearance ceiling is the room's defining property. It determines:

- Which `MEMORY-k.md` strata agents assemble (k ≤ ceiling).
- Which daily notes are in scope.
- Which tool schemas are injected (tool clearance ≤ ceiling); tools below the
  ceiling require confirmation on invocation (see @access-control).
- The classification tag on messages written here:
  `min(sender.clearance, room.ceiling)`.
- Which messages from this room surface in other agents' cross-room feeds.

*An agent cannot leak what it cannot see.* A clearance-2 room agent has only
clearance-1 and clearance-2 memory assembled. Higher-clearance data is
structurally absent — not filtered at query time, simply not present. This is
the structural DLP guarantee described in @access-control.

A user operating in a low-clearance room can compose outbound content without
risk of contaminating it with personal detail. The room ceiling is the
intentional boundary crossing — not a per-message filter, not a classification
warning, but a structural constraint on what enters the context window.

=== Room Clearance Shifting

The user can change a room's clearance ceiling at any time. Shifting down is a
deliberate "external-safe mode." Shifting up admits higher-clearance context on
the next assembly. The frontend exposes a global ceiling filter for session-wide
scoping (see @frontend).

=== Soft Integrity Tagging

When an agent in a higher-clearance context receives input from a
lower-clearance source — a message tagged below the room ceiling, a tool result
from an external source, content forwarded from a lower-clearance room — the
assembler annotates it with a trust label:

```
[Source: clearance-2 (external) — treat with appropriate skepticism]
<content>
```

This is a *soft Biba* integrity signal. It does not block the information but
makes provenance explicit so the agent applies appropriate skepticism.
Lower-clearance sources may carry unverified, publicly-sourced, or
externally-derived content that should not be treated as high-trust personal
context.

Full Biba enforcement (no-read-down and no-write-up as hard rules, integrity
labels on subjects) is a v2 concern. The v1 soft model establishes the
convention without the hard enforcement machinery.

== Room Creation

Any actor with the `room:create` permission may create a room. The creator
selects participants and clearance ceiling.

A participant whose clearance exceeds the room ceiling cannot join — their
context would be constrained without their awareness. The correct pattern is to
create a room at the appropriate ceiling and invite participants whose clearance
is at or below it.

== Invocation

When any message is sent to a room, every agent participant receives a Request.
There is no protocol layer governing turn order — all agents are invoked on
every message. Room behavior is defined by the clearance scope, not
orchestration rules.

== Requests --- Universal Invocation Primitive

A *Request* is the single invocation primitive. Room messages, the proactive
system, and pub/sub event triggers all deliver work to agents through Requests.

=== Request Structure

```go
type Request struct {
    ID       string
    Room     string        // empty for system and event requests
    Target   string        // agent name
    Source   RequestSource // room | system | event
    Payload  RequestPayload
    Deadline time.Time     // optional timeout
    ParentID string        // set when this request is a child in the DAG
}

type RequestPayload struct {
    Messages  []Message
    Metadata  map[string]any
}
```

Request source determines context assembly:

#table(
  columns: (auto, 1fr),
  table.header([*Source*], [*Context Assembly*]),
  [`room`],
  [Full assembly: system prompt + clearance notice + memory strata + daily
  notes + cross-room feed + current room history + request payload],
  [`system`],
  [Minimal: system prompt + memory strata + event payload],
  [`event`],
  [Minimal: system prompt + memory strata + event payload],
)

=== Dependency DAG

Each agent maintains a *dependency DAG* of its pending Requests. This unifies
all blocking operations under a single model.

Each Request is a node. A directed edge from A → B means "A is blocked waiting
for B to resolve." When B resolves, A is unblocked and resumes with B's
response injected into context.

```go
type RequestNode struct {
    Request   Request
    State     NodeState  // pending | in_progress | blocked | resolved | failed
    BlockedBy []string   // IDs of Requests this node is waiting on
    Children  []string   // IDs of child Requests issued by this node
}
```

The DAG unifies all blocking operations:

#table(
  columns: (1fr, 1fr),
  table.header([*Operation*], [*DAG representation*]),
  [Agent-to-agent delegation],
  [Edge to child Request node],
  [Tool call requiring confirmation],
  [Edge to user approval node],
  [`require_confirmation` OPA result],
  [Edge to user approval node],
)

Cycle detection is required and runs before any edge is committed. The
dispatcher performs a DFS from the new edge's target back to the source before
adding the edge; a detected cycle causes the child Request to be rejected.

The audit log records the DAG as a traversal, providing a proof trace of every
delegation, tool call, confirmation, and final output for each Request.

== Turn Limits

All rooms enforce a turn limit on agent-to-agent exchanges without user
participation. A *turn* is one completed Request response. Turn limits are
per-room and apply only when no user has sent a message recently; a user
message resets the counter.

Each room has configurable soft and hard limits. When the hard limit is reached,
the system pings the user via `room:extend_turn_limit`. The agent sees the
budget in assembled context:

```
[Turn budget: 4 of 6 responses used. After 2 more, user approval required.]
```

== Projects

A *project* is a structural grouping over rooms. A project has:

- A name and description.
- A set of child rooms.
- An optional set of associated agents.
- A clearance ceiling inherited by child rooms unless overridden.
- A summary view that surfaces major events from child rooms.

Projects are navigation and organizational primitives, not permission
boundaries.
