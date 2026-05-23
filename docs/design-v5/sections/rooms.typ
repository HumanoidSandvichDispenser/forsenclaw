= Rooms and Protocols <rooms>

Rooms and protocols are separate concepts. A *room* is a transcript container
with participants. A *protocol* is a behavioral contract that governs how and
when agents are invoked via Requests. Rooms are infrastructure; protocols are
orchestration policy.

== Rooms

A room has:

- A *participant list* (actors --- users and/or agents).
- A *transcript file* (JSONL on disk).
- A *clearance ceiling* --- the highest clearance level any message may carry,
  and the scope at which agents assemble context when operating in this room.
- A *protocol* --- the behavioral contract governing the room.
- A *parent* (optional) --- for structural grouping under a project.

Room metadata (participant list, clearance ceiling, protocol type and
configuration, protocol state) is stored in SQLite for queryability. The
transcript itself is the JSONL file.

== Room Creation

Any actor with the `room:create` permission may create a room. The creator
selects initial participants, clearance ceiling, and protocol configuration.

A participant whose clearance is below the room's ceiling cannot join. Adding a
participant whose clearance is below existing message classifications is not
permitted --- the resolution is to create a new room with an explicit
cross-clearance handoff to seed it.

== Requests --- Universal Invocation Primitive

A *Request* is the single invocation primitive. Room protocols, the proactive
system, and pub/sub event triggers all deliver work to agents through Requests.
There is no separate invocation path.

=== Request Structure

```go
type Request struct {
    ID       string
    Room     string        // empty for system and event requests
    Target   string        // agent name (or "*" for broadcast)
    Source   RequestSource // room | system | event
    Payload  RequestPayload
    Deadline time.Time     // optional timeout
    ParentID string        // set when this request is a child in the DAG
}

type RequestSource string

const (
    RequestSourceRoom   RequestSource = "room"
    RequestSourceSystem RequestSource = "system"
    RequestSourceEvent  RequestSource = "event"
)

type RequestPayload struct {
    // Messages the agent should respond to
    Messages      []Message
    // Interjections that arrived while another agent was responding
    Interjections []Message
    // Protocol-specific metadata (iteration count, round number, etc.)
    Metadata      map[string]any
}
```

Request source determines context assembly:

#table(
  columns: (auto, 1fr),
  table.header([*Source*], [*Context Assembly*]),
  [`room`],
  [Full assembly: system prompt + memory strata + daily notes + cross-room feed
  + current room history + request payload],
  [`system`],
  [Minimal: system prompt + memory strata + event payload],
  [`event`],
  [Minimal: system prompt + memory strata + event payload],
)

=== Protocol Interface

```go
type Protocol interface {
    // Start is called when the room is created or protocol is activated.
    Start(room *Room, dispatcher Dispatcher)

    // OnMessage is called when a message lands in the room.
    OnMessage(room *Room, sender Actor, msg Message)

    // OnRequestResponse is called when an agent completes its Request response.
    OnRequestResponse(room *Room, req Request, response Message)

    // OnInterjection is called when a non-targeted actor sends a message
    // while a Request is in flight.
    OnInterjection(room *Room, sender Actor, msg Message)

    // ShouldTerminate returns true if the protocol's end condition is met.
    ShouldTerminate(room *Room) bool

    // State returns serializable protocol state for persistence.
    State() ProtocolState

    // Restore reconstructs protocol state from persisted data.
    Restore(state ProtocolState)
}
```

The `Dispatcher` interface is how protocols issue Requests:

```go
type Dispatcher interface {
    IssueRequest(req Request) error
    BroadcastRequest(room *Room, payload RequestPayload) error
}
```

== Built-In Protocols

=== FreeForm

For two-participant rooms only. When participant A sends a message, the
protocol issues a Request to participant B. Suitable for DMs and pair
conversations (user + agent, or agent + agent).

FreeForm rooms are limited to exactly two participants. Multi-agent
conversations require a structured protocol.

Configuration:
- `max_turns`: hard limit on agent-to-agent exchanges without user
  participation (safety mechanism for agent + agent freeform rooms).

=== RoundRobin

Participants speak in a defined order. The protocol issues a Request to each
participant sequentially, waiting for each response before issuing the next.

Configuration:
- `turn_order`: list of participant names.
- `max_rounds`: hard limit on full rotations.
- `include_user`: whether the user gets a turn slot or can only interject.

This is the protocol that enables structured debate. Two agents with opposing
system prompts in a RoundRobin room _is_ the debate feature --- no special room
type needed.

=== Broadcast

All agent participants receive simultaneous Requests and respond independently.
The protocol collects all responses before proceeding to the next round.

Configuration:
- `max_rounds`: hard limit on broadcast rounds.
- `reconciliation`: whether a designated agent synthesizes after collection.
- `reconciler`: which agent reconciles (if enabled).

This is the protocol for planning poker, independent estimation, parallel
review --- any workflow where agents should respond without seeing each other's
answers first.

=== IterativeDraft

Collaborative creation with user in the loop. Operates in cycles: the protocol
issues a Request to the drafter, presents the draft to the user, waits for user
feedback, then issues a new Request with the feedback.

Configuration:
- `max_iterations`: hard limit on draft cycles.
- `drafter`: which agent drafts.
- `auto_close_on_accept`: whether the room closes when the user accepts.

=== FireAndForget

Task delegation. One actor sends a task; the protocol issues a single Request
to the executor agent. The room closes on response delivery or timeout.

Configuration:
- `timeout`: duration before forced close.
- `result_to`: where the result is forwarded (a room, or the spawning actor's
  DM).

== User Messages and Interjections

Users are not invoked via Request --- they send messages whenever they choose.
The protocol handles user messages through `OnMessage` and `OnInterjection`:

- *In FreeForm rooms:* A user message triggers a Request to the other
  participant. This is the normal flow.
- *In RoundRobin/Broadcast rooms:* A user message while the protocol is
  awaiting an agent's Request response is queued as an interjection. The
  interjection is included in the current or next Request payload so the agent
  sees it. If the user has a turn slot (`include_user=true`), their turn is
  handled by the protocol waiting for a user message instead of issuing a
  Request.
- *In IterativeDraft rooms:* User messages are the feedback input the protocol
  waits for between draft cycles.

If an agent is mid-response when an interjection arrives, the interjection is
queued until the agent's current response completes, then included in the next
Request.

== Dependency DAG

Each agent maintains a *dependency DAG* of its pending Requests rather than a
flat queue. This unifies all blocking operations under a single model.

=== DAG Structure

Each Request is a node. A directed edge from A → B means "A is blocked waiting
for B to resolve." When B resolves, A is unblocked and resumes with B's
response injected into its context.

```go
type RequestNode struct {
    Request    Request
    State      NodeState  // pending | in_progress | blocked | resolved | failed
    BlockedBy  []string   // IDs of Requests this node is waiting on
    Children   []string   // IDs of child Requests issued by this node
}
```

=== Flow

+ Agent picks up an unblocked Request from its DAG.
+ If the agent needs input from another agent, it issues a child Request and
  marks the original as blocked with an edge to the child.
+ Agent continues processing other unblocked Requests while waiting.
+ When the child resolves, the parent is unblocked and resumes with the
  child's response injected into context.

=== Blocking Operations Unified

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
  [Multi-turn within a single response],
  [Edges to each sequential turn node],
)

=== Cycle Detection

Cycle detection is required and runs before any edge is committed. Agent A
waiting on B waiting on A must be detected and failed fast to prevent
deadlock. The dispatcher performs a DFS from the new edge's target back to the
source before adding the edge; if a cycle is found, the child Request is
rejected with an error.

=== Audit Integration

The audit log records the DAG as a traversal. Every Request includes its full
parent/child chain, providing a proof trace of everything that happened to
resolve a Request: delegations, tool calls, confirmations, and final output.

== Turn Limits

Regardless of protocol, all rooms enforce a turn limit on agent-to-agent
exchanges without user participation. This is a safety mechanism, not a
protocol feature.

- A *turn* is one completed Request response.
- Turn limits are per-room.
- Turn limits apply only when no user has sent a message recently. A user
  message resets the counter.
- Each room has configurable soft and hard limits.

When the hard limit is reached, the protocol stops issuing Requests. To
continue, it pings the user via `room:extend_turn_limit`. The agent sees the
budget in its assembled context:

```
[Turn budget: 4 of 6 responses used. After 2 more, user approval required.]
```

== Projects

A *project* is a structural grouping over rooms, not a room itself. A project
has:

- A name and description.
- A set of child rooms.
- An optional set of associated agents (persistent agents relevant to the
  project's domain).
- A clearance ceiling inherited by child rooms unless overridden.
- A summary view that surfaces major events from child rooms.

Projects are navigation and organizational primitives, not permission
boundaries.

== Composing Interaction Patterns

The protocol model means you don't need named room types to get complex
behavior. Examples:

- *Adversarial debate:* RoundRobin room, two agents with opposing prompts,
  `max_rounds=10`. User observes or interjects.
- *Planning poker:* Broadcast room, N agents estimating independently, then a
  reconciliation round.
- *Document workshop:* IterativeDraft room, one drafter agent, user as
  feedback source.
- *Task delegation:* FireAndForget room, orchestrator sends task, ephemeral
  agent executes.
- *DM chat:* FreeForm room, user + agent.
- *Agent pair conversation:* FreeForm room, agent + agent, with turn limit as
  safety.

New patterns emerge from new protocol implementations, not new primitives.
