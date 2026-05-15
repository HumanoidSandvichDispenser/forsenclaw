# Hearth — Design Document (v3)

A self-hosted multi-agent orchestration system with layered persistent memory,
protocol-driven rooms, and capability-based access control.

---

## 1. Overview

Hearth orchestrates multiple LLM agents — local and cloud — across persistent
layered memory, protocol-driven conversation rooms, and a capability system that
separates what an actor can *see* from what an actor can *do*. It runs on a NAS
or similar always-on host, with optional thin client daemons on user devices for
local inference and localhost-bound tool access.

Users and agents are both **actors** in the system. The only structural
difference: users authenticate with credentials; agents are defined by
configuration and invoked by the system. Both participate in rooms, send
messages, hold permissions, and are subject to clearance. This unification
simplifies the interaction model — a room doesn't care whether its participants
are humans or agents.

The design supports both **companion-style** agents (long-lived identity,
accumulated memory, proactive behavior) and **task-style** agents (ephemeral,
scoped context, reactive). These are configurations of the same agent primitive,
controlled by per-agent feature flags.

### Goals

- Persistent agent identities that accumulate memory across sessions.
- Clean separation between agent identity and the underlying model.
- Two orthogonal access axes — clearance (data) and permissions (actions) —
  enforced at distinct boundaries.
- Multi-tier model routing per agent (primary / routine / sensitive).
- MCP as the universal tool integration surface.
- Protocol-driven rooms that compose behavioral contracts onto conversations.
- Self-hosted, single-user, sovereign over its own data. No required cloud
  dependency.
- Single binary. Target idle footprint: ~50–80 MB.

### Non-goals

- Not a multi-tenant SaaS or cloud product.
- Not a replacement for coding harnesses; Hearth integrates them as MCP tool
  surfaces.
- Not a single-model chatbot wrapper.
- Not a competitor to general-purpose task orchestration frameworks (LangChain,
  AutoGen). Hearth's distinguishing feature is the unified companion + task
  model under a single permission and memory architecture.

### Design principles

- **Identity lives in memory, not in context.** Agents feel continuous because
  their memory layers are continuous; the active context window is bounded and
  assembled per-interaction.
- **Defense in depth at the boundary, not the storage.** Memory is stored at
  full fidelity; clearance is enforced at retrieval, message send, and context
  injection — not by deleting or summarizing data on disk.
- **Stateless models, stateful agents.** Models are compute primitives. Agents
  are the persistent identity that owns context, memory access, and permissions.
- **No ambient authority.** Every privileged action — tool dispatch, room
  creation, memory write, settings change — is gated by an explicit permission
  grant.
- **Trust is at the agent, not the model.** An agent's clearance determines
  what data it may see. Within that clearance, the agent may route calls to any
  of its configured models.
- **Protocols compose; primitives don't multiply.** Behavioral contracts are
  properties of rooms, not distinct room types. New interaction patterns are new
  protocols, not new primitives.

### Tech stack

- **Backend:** Go
  - Concurrency: goroutines + channels. Agent queues are `chan Message`; drain
    loops are goroutines.
  - Database driver: standard `database/sql` with pgx (Postgres) and
    modernc.org/sqlite (pure-Go SQLite, no CGO).
  - Migrations: goose or golang-migrate.
  - Scheduling: time.Ticker + priority queue for proactive ticks.
- **Frontend:** Vue 3 + TypeScript + Pinia
- **Database:**
  - Development: SQLite for everything.
  - Production: SQLite for memory layers (episodic, semantic — file-local,
    portable). Postgres for agent definitions, audit log, room metadata, user
    data.
- **Local inference:** Ollama over HTTP (not in-process).

Agent runtime state — message queues, session scratchpads, worker lifecycle —
lives in-process as Go objects. Durability across server restarts comes from
periodic checkpoints to episodic, not from a distributed queue broker.

---

## 2. Actor Model

An **actor** is any entity that participates in rooms, sends messages, and is
subject to clearance and permissions. There are two kinds:

### Users

A user authenticates with credentials (v1: username + password; later: hardware
token, biometric). On authentication, the user receives a session token scoped
to the connection. The user is the **root identity** — they can do anything:
spawn agents, modify settings, grant or revoke permissions, read any audit log,
terminate the server, read or write any memory layer. Root access is identity,
not a permission grant.

### Agents

An agent is defined by configuration and invoked by the system. An agent has a
role, model assignments, feature flags, clearance, and permissions. Agents do
not authenticate — they are instantiated by the server from their stored
definitions.

### What they share

Both users and agents:

- Participate in rooms as message senders and recipients.
- Are subject to room protocols (turn-taking, iteration limits).
- Have a clearance tier that governs data visibility.
- Have permissions that govern actions (though users hold implicit root).
- Appear in room transcripts with their identity as the sender.

### What differs

| Concern | User | Agent |
|---|---|---|
| Authentication | Credentials | Definition + invocation |
| Authority | Root (implicit) | Granted permissions |
| Memory ownership | N/A (user is the subject of memory) | Per-agent semantic + scratchpad |
| Clearance | Implicitly top-tier | Configured per-agent |
| Lifecycle | Login/logout | Persistent or ephemeral |

This unification means the frontend treats DMs with agents and multi-participant
rooms identically — like Discord. A DM with the housewife is just a room with
two participants. A room with three agents and the user is the same primitive.

---

## 3. Agent Model

An **agent** is a persistent identity defined by:

1. A **role** (name, system prompt, behavioral configuration).
2. A **multi-tier model assignment** for routing different kinds of work.
3. A set of **feature flags** that determine how the agent uses memory, context,
   and proactive triggers.

The agent owns its context window, its memory access rights, its permission set,
and its identity across sessions. Models are stateless compute that the agent
invokes.

### Multi-tier model assignment

Each agent declares three model tiers. Tiers describe **routing roles**, not
trust boundaries — an agent's clearance governs what data it sees regardless of
which tier handles the call.

- `primary_model` — high-reasoning tasks. Examples: Claude Sonnet, GPT-class.
- `routine_model` — low-stakes ticks, proactive checks, summarization.
  Examples: local Gemma, local Llama.
- `sensitive_model` — the model for operations the user prefers to keep local
  or routed to a trusted provider. Typically local. Used by default for
  redaction proposals and other sensitivity-aware operations.

The agent decides which tier to invoke based on the task. This is not model
fallback — tiers have different roles, not different quality levels.

### Feature flags

| Flag | On | Off |
|---|---|---|
| `session_spanning_context` | Session scratchpad persists across rooms within a session | Fresh context from durable memory only per room |
| `raw_episodic_recall` | RAG may retrieve raw episodic logs | Retrieves only from distilled semantic memory |
| `identity_continuity` | Semantic memory persists across sessions | Semantic memory per-session; starts fresh |
| `proactive_triggers` | Runs scheduled and interrupt-driven proactive ticks | Acts only when invoked |

Companion-style agents typically have all four on. Task-style agents typically
have all four off. The flags are independent — not all combinations need be
common, but they must be coherent.

### Context ownership

Each agent has exactly one session scratchpad. Room membership does not multiply
scratchpads. When an agent participates in multiple rooms with
`session_spanning_context=on`, the same scratchpad spans those rooms, with room
name carried as metadata on each entry so the agent can distinguish source and
destination.

When `session_spanning_context=off`, no scratchpad is maintained; the agent
assembles a context window per-invocation from memory layers and the current
message batch.

This is the central architectural decision: agent continuity is decoupled from
context window persistence. An agent can feel continuous without carrying its
entire history in active tokens.

### Agents are not knowledge specialists

LLMs already have broad knowledge. The role of a persistent agent is not "knows
about cooking" or "knows about code" — it is defined by **what it knows about
the user** (clearance), **what it can do** (permissions), and **what it's
responsible for** (role). The housewife is the most trusted agent not because it
knows the most about the world, but because it knows the most about *you*. A
lower-clearance agent isn't dumber — it has less context about your life.

---

## 4. Memory Architecture

Hearth has four memory layers, divided along two axes: durable vs. transient,
and structural vs. retrieval. Each serves a different function with different
access patterns. **Memory is owned by agents, not rooms.**

### Core facts file

A small curated markdown document, manually editable, always injected at the
start of any agent context assembly. Functions like a Claude project document:
high-signal, low-volume, stable.

The core facts file is the floor of an agent's knowledge — user identity,
hardware, persistent project state, the agent's own role and constraints.

Auto-updates are conservative: the distillation pass at session end may propose
updates, but the user reviews and accepts on a non-blocking schedule. Manual
editing is the primary update path.

### Session scratchpad

A structured, agent-writable artifact that persists across rooms within a single
session, for agents with `session_spanning_context=on`. Contents are
agent-determined: distilled facts from the session so far, in-progress
reasoning, references to retrieved content.

The scratchpad is *not* the assembled context — it is one input to context
assembly. The scratchpad is also not raw conversation log (that is episodic). It
sits between them: structured enough to carry across rooms without ballooning,
durable enough to survive inactivity.

At session end, the scratchpad is distilled into semantic memory and discarded.

### Episodic memory

Append-only SQLite log of full raw conversations. Stored on disk indefinitely
(subject to retention policy). Never injected raw into a context window by
default — episodic is for audit, reprocessing, and clearance-permitted
retrieval.

Episodic stores everything: user messages, agent responses, tool calls, tool
results, system events. Schema is conversation-shaped with a per-message
clearance tag attached at write time so retrieval can filter without parsing.

### Semantic memory

Extracted facts, preferences, and structured knowledge distilled from episodic
and session scratchpads by a local model at session end. Preferences are just
facts about the user — no separate preference layer.

Semantic memory is the primary RAG retrieval target. Embeddings are computed
locally (Ollama + embedding model). Retrieval is by vector similarity plus
optional metadata filters (clearance tier, recency, source agent, room).

**Ownership:** Semantic memory is per-agent. An agent's distillation produces
semantic entries tagged to that agent. An agent in five rooms has one semantic
store, not five. Cross-agent retrieval is possible where clearance permits, but
the default is agent-scoped.

### Sessions and lifecycle

A **session** is a bounded period of agent activity.

- Ends after **30 minutes of inactivity** on a per-agent basis.
- Within a session: agents with `session_spanning_context=on` carry the
  scratchpad across rooms.
- At session end: scratchpad and uncommitted conversation commit to episodic; a
  local-model distillation pass updates semantic memory; significant updates are
  proposed for core facts; scratchpad is discarded.
- Proactive actions run in isolated session contexts — they do not extend or
  contaminate user-facing sessions.

### Checkpointing

Activity-based checkpoints commit conversation deltas and scratchpad snapshots
to episodic every N messages (configurable; default 20). Checkpoints are raw
writes — no summarization. Purpose is crash durability. If the server dies
mid-session, the next start reconstructs from the most recent checkpoint.

### Retrieval rules

Each retrieval is parameterized by the requesting agent's clearance tier.
Clearance is numbered ascending — higher = more trust, more sensitive data:

- Agent with clearance ≥ N may retrieve semantic memory tagged ≤ N.
- Agent with `raw_episodic_recall=on` and clearance ≥ N may additionally
  retrieve episodic tagged ≤ N.
- Retrieval above an agent's clearance is filtered at the query layer — the
  agent never sees what it should not.

Clearance tags are applied at write time, not retrieval time.

---

## 5. Rooms and Protocols

Rooms and protocols are separate concepts. A **room** is a transcript container
with participants. A **protocol** is a behavioral contract that governs how
messages flow through a room. Rooms are infrastructure; protocols are policy.

### Rooms

A room has:

- A **participant list** (actors — users and/or agents).
- An **ordered message history** (the transcript).
- A **clearance ceiling** — the highest clearance tier any message may carry.
- A **protocol** — the behavioral contract governing the room.
- A **parent** (optional) — for structural grouping under a project.

Rooms do not own context. Context is per-agent (§3). A room is a routing
coordinate and a shared transcript.

### Room creation

Any actor with the `room:create` permission may create a room. The creator
selects initial participants, clearance ceiling, and protocol.

A participant whose clearance is below the room's ceiling cannot join. Adding a
participant whose clearance is below existing message classifications is not
permitted — the resolution is to create a new room with an explicit
cross-clearance handoff to seed it (§7).

Default rooms are provided out of the box. Users may rename or reconfigure any
room.

### Protocols

A protocol is a named behavioral contract with the following interface:

```go
type Protocol interface {
    // ValidateSend checks whether this message is permitted right now.
    // Returns an error if the protocol disallows it (e.g. not your turn).
    ValidateSend(room *Room, sender Actor, msg Message) error

    // OnMessageSent is called after a message lands. The protocol may
    // update its internal state (turn counters, iteration count, etc).
    OnMessageSent(room *Room, sender Actor, msg Message)

    // ShouldTerminate returns true if the protocol's end condition is met.
    ShouldTerminate(room *Room) bool

    // OnTerminate runs cleanup when the room closes under this protocol.
    OnTerminate(room *Room)
}
```

Protocols are enforced by the dispatcher, not by the agents. An agent that
tries to send a message the protocol disallows receives a rejection it can
reason about.

### Built-in protocols

**FreeForm** — no constraints on who speaks when. Default protocol. Suitable
for chat, DMs, open-ended collaboration.

**TurnTaking** — participants speak in a defined order. Configurable:
- `turn_order`: list of participant names, or `round_robin`.
- `max_rounds`: hard limit on full rotations before the room requires user
  intervention or closes.
- `allow_pass`: whether a participant may pass their turn.

This is the protocol that enables adversarial debate, planning poker, and
any structured multi-agent deliberation. Two agents with opposing system
prompts in a TurnTaking room *is* the debate feature — no special room type
needed. User can also be included.

**IterativeDraft** — designed for collaborative creation with user in the loop.
Operates in cycles: draft → feedback → revision. Configurable:
- `max_iterations`: hard limit on draft cycles.
- `feedback_actor`: which participant provides feedback (typically the user).
- `auto_close_on_accept`: whether the room closes when the user accepts.

This is the protocol for writing workshops, document review, plan refinement —
any workflow where an agent produces, the user reacts, and the agent revises.

**FireAndForget** — task delegation. One actor sends a task; one agent
executes and posts a result. The room closes on result delivery or timeout.
Configurable:
- `timeout`: duration before forced close.
- `result_to`: where the result is forwarded (a room, or the spawning actor's
  DM).

### Custom protocols

Protocols are Go interfaces. A user (or eventually a plugin system) can define
new protocols by implementing the interface. The dispatcher doesn't care what
the protocol does internally — it calls `ValidateSend` before sending and
`ShouldTerminate` after.

### Turn limits (cross-protocol)

Regardless of protocol, all rooms enforce a turn limit on agent-to-agent
exchanges without user participation. This is a safety mechanism, not a
protocol feature.

- A **turn** is one full agent response.
- Turn limits are per-agent per-room.
- Turn limits apply only when no user has sent a message recently. A user
  message resets the counter.
- Each room has configurable soft and hard limits.

When the hard limit is reached, the agent is blocked from sending. To continue,
it requests an extension via `room:extend_turn_limit`, which surfaces to the
user as a notification. The agent sees the budget in its assembled context:

```
[Turn budget: 4 of 6 responses used. After 2 more, user approval required.]
```

### Projects

A **project** is a structural grouping over rooms, not a room itself. A project
has:

- A name and description.
- A set of child rooms.
- An optional set of associated agents (persistent agents that are relevant to
  the project's domain).
- A clearance ceiling inherited by child rooms unless overridden.
- A summary view that surfaces major events from child rooms (new results,
  completed tasks, stalled debates).

Projects solve the "bunch of rooms gets messy" problem. The language-learning
scenario spawns several rooms (tutoring chat, conversation practice, vocabulary
review); a project groups them and gives a single entry point.

Projects are not containers in the permission sense — a room's permissions are
its own. Projects are a navigation and organizational primitive.

### Composing interaction patterns

The protocol model means you don't need named room types to get complex
behavior. Examples:

- **Adversarial debate:** TurnTaking room, two agents with opposing prompts,
  `max_rounds=10`. User observes or participates.
- **Planning poker:** TurnTaking room, N agents each estimating independently,
  then a reconciliation round. User reviews.
- **Document workshop:** IterativeDraft room, one drafter agent, user as
  feedback_actor.
- **Task delegation:** FireAndForget room, orchestrator sends task, ephemeral
  agent executes.
- **Open chat:** FreeForm room, any participants.

New patterns emerge from new protocol implementations, not from new primitives.

---

## 6. Agent Messaging and Invocation

### Queue model

Each agent owns a single **message queue** — a global, ordered buffer of all
incoming messages and events across all rooms. The queue is the sole input
channel. Nothing invokes an agent except via the queue.

Queue entry shape:

```go
type QueueEntry struct {
    Index     uint64    // monotonically increasing, global to agent
    Type      string    // "message" | "event"
    Timestamp time.Time
    Room      string    // room ID; empty for system events
    Sender    string    // actor name
    To        []string  // v1: exactly one recipient
    Payload   string    // message body or event payload
}
```

`type: message` covers user and inter-agent messages. `type: event` covers
proactive ticks, system signals, and automation wakeups. Both share the same
queue and processing loop.

### Wakeup and sleep

An agent is either **awake** (goroutine running, draining queue) or **asleep**
(no goroutine).

- Wakes when a queue entry arrives and no goroutine is running.
- Stays awake while the queue is non-empty.
- Sleeps when the queue is fully drained.

Implementation: per-agent goroutine. Enqueue sends on the agent's channel; if
no goroutine is active, one is spawned. The goroutine drains until the channel
is empty. Before exiting, it re-checks the channel under a lock — if non-empty
(a message arrived during the exit sequence), it continues rather than exiting.
This is a lock-free singleton per agent.

### Drain and batching

Processing loop:

1. Dequeue the head entry. Its `Room` field determines the **target room**.
2. Collect all remaining entries with the same room — the **respond-to batch**.
3. Remaining entries form the **ambient queue**.
4. **Check protocol**: call `room.Protocol.ValidateSend()` preemptively — if
   the protocol wouldn't let this agent respond right now (e.g., it's not their
   turn), skip this batch and try the next head entry for a different room. The
   skipped entries stay in the queue.
5. Assemble context and invoke the model.
6. Validate the response through the protocol before sending.
7. Remove processed entries from the queue.
8. Return to step 1.

Step 4 is the protocol integration point. TurnTaking rooms may block an agent
whose turn hasn't arrived yet. The drain loop handles this by moving to the
next room's entries rather than blocking. The skipped entries are retried on the
next drain cycle (triggered when the protocol state changes — e.g., the other
participant finishes their turn and the room notifies waiting agents).

### Queue presentation to the model

The model sees two sections:

**Respond-to section** — the current batch, presented as the active task.

**Ambient section** — remaining queue entries for other rooms, presented as
background context. The model is instructed not to act on these. Capped at 20
entries; overflow noted in a footer.

Global indices persist across invocations, giving the model temporal context.

### Context assembly

Assembled fresh each invocation. Order:

1. **Core facts file** — always first.
2. **RAG retrieval** — semantic memory queried against the respond-to batch.
   Raw episodic additionally if `raw_episodic_recall=on` and clearance permits.
3. **Skill files** — top-K matched against the respond-to batch.
4. **MCP tool schemas** — the agent's permitted tools, resolved and injected
   (see §9).
5. **Session scratchpad** — if `session_spanning_context=on`.
6. **Room history** — target room's message history (excluding respond-to
   batch), up to configured window.
7. **Turn budget notice** — remaining turns before extension required.
8. **Queue presentation** — respond-to then ambient.

### SLM harness considerations

The invocation pipeline is the correct layer for small-model scaffolding.
Techniques for SLMs (per-turn skill injection, output repair for malformed tool
calls, quality monitoring for empty/looping responses, thinking-budget caps)
apply at the dispatcher level, transparent to the agent definition.

---

## 7. Access Control

Clearance and permissions are **orthogonal** axes with distinct enforcement
points. Clearance is a security construct, not a domain-knowledge label.

### Clearance — what an actor can see

Clearance is a tiered data classification. Tiers form a totally ordered set of
integers; higher numbers mean more trust and more sensitive data. The number of
tiers and their meaning is user-configured. A system can use as few as two
(trusted vs. untrusted) or many more. Tier values need not be contiguous.

Default five-tier scheme:

- **Tier 5** — full access. User and most-trusted agent (named housewife in
  this document). Sees everything.
- **Tier 4** — broad access with sensitive-data exclusions (credentials,
  financial, etc).
- **Tier 3** — task-scoped. May see specific projects or topics, not general
  life context.
- **Tier 2** — public-only. Sanitized inputs. Suitable for agents hitting
  external APIs.
- **Tier 1** — minimum trust. Should never see user data.

Users can add, remove, renumber, or rename tiers. The dispatcher validates
agent definitions and message classifications against the current tier set.

Clearance is enforced at three points:

1. **Retrieval** — every RAG query filters by requesting actor's tier.
2. **Outgoing message send** — dispatcher checks message classification against
   destination room ceiling. Rejected if above. This prevents cross-room
   leakage from an agent with `session_spanning_context=on` whose scratchpad
   contains higher-classified information.
3. **Spawn-time context injection** — context passed to a child agent must not
   exceed the child's clearance. Dispatcher rejects violating spawns.

Classification at write time: a message sent in a tier-3 room is tagged tier-3
in episodic and never appears in a tier-2 retrieval. Default classification is
the sender's clearance, capped at the room's ceiling. Agents can explicitly
down-classify when content genuinely doesn't carry sensitivity from its source.

### Explicit cross-clearance handoff

When an agent deliberately wants to pass sensitive content down — e.g., the
housewife passing a redacted email summary to a tier-2 API agent:

```
propose_handoff(
    source_content: <high-clearance content>,
    target_clearance: <lower tier>,
    destination: <agent or room>,
) -> proposed_redaction
```

The agent generates a proposed redaction using its `sensitive_model`. The
dispatcher shows original and proposed redaction to the user side-by-side. User
reviews, optionally edits, and approves or rejects. Only on approval does the
redacted content cross the boundary.

This is intentionally not the default. Everyday cross-clearance protection is
structural (the outgoing-message check). This is for deliberate declassification.

### Permissions — what an actor can do

Permissions are fine-grained action capabilities, IAM-style. Each statement
has:

- **Action** — the gated operation (e.g. `settings:write[wallpaper]`).
- **Scope** — optional refinement in brackets. Literal, glob, or
  comma-separated list.
- **Effect** — `allow`, `require_confirmation`, or `deny`.

Evaluation order:

1. Default deny everything.
2. Explicit `deny` overrides all. Unconditional.
3. Most specific scope match wins. Literal > glob > wildcard.
4. On tie: `require_confirmation` wins over `allow` (more restrictive).

Permission categories:

- `tool:invoke[<tool_id>]` — MCP tool dispatch.
- `room:create`, `room:add_participant`, `room:close`,
  `room:extend_turn_limit`.
- `memory:write[<layer>]`, `memory:retrieve[<layer>]`.
- `agent:spawn[<role>]`, `agent:terminate`, `agent:compact[<target>]`.
- `settings:read[<scope>]`, `settings:write[<scope>]`.
- `proactive:enable`, `proactive:act[<risk_tier>]`.
- `handoff:propose[<target_clearance>]`.
- `audit:read`.

Role bundles ("orchestrator", "observer", "task_worker") exist as UX presets
that expand to permission sets. Enforcement is per-action at the dispatcher.

### Audit

The audit log records **actions taken** and **attempts denied**:

- Tool invocations (call + arguments + result status).
- Permission denials.
- `require_confirmation` checks (request + user response).
- Settings changes.
- Agent spawn and teardown.
- Cross-clearance rejections.
- Handoff proposals and resolutions.

The log does *not* record every retrieval or routine clearance filter. These are
runtime mechanics, not actions.

Append-only, Postgres-backed, accessible to the user without restriction.
`audit:read` is its own permission for agents; they don't get it by default.

---

## 8. Agent Lifecycle

### Persistent agents

Long-lived. Configured once, exist indefinitely. Stable identity, accumulated
memory, long-term permission grants. The user typically has 1–3.

The housewife is the default persistent agent — highest clearance, broadest
permissions, most accumulated context about the user. It is not an
"orchestrator" architecturally — it's just the most trusted agent, and it's
good at orchestration as a consequence of knowing the most. It can also just
chat, or be told things, or asked non-routing questions. Users are not
required to have an orchestrator; they can talk directly to any agent.

Persistent agents may not self-terminate without user confirmation —
loss of accumulated memory context is irreversible.

### Ephemeral agents

Spawned for a task, given scoped context, terminated on completion. They do not
accumulate their own semantic memory (though transcripts commit to episodic on
teardown for audit and parent reference).

Characterized by:

- Spawned by a persistent agent or the user.
- Inherit a subset of the spawner's permissions, never more.
- Clearance tier equal to or lower than the spawner's.
- Scoped context injection at spawn (not a full memory dump).
- Discarded after task completion, timeout, room closure, or revocation.

Ephemeral agents are where bulk work happens. Most tasks need a focused
context, a defined goal, and teardown — not a long-lived identity.

### Spawn

The spawner must hold `agent:spawn[<role>]`. The spawner supplies:

- Task description.
- Context injection (dispatcher validates against child's clearance).
- Permission set (subset of spawner's).
- Timeout or termination condition.
- Whether the agent joins an existing room or gets a new one.

### Teardown

Triggers: task completion, timeout, room closure, revocation by parent or user.

On teardown:

- Scratchpad discarded after final commit.
- Full transcript committed to episodic with clearance tags.
- Role definition persists for future spawns.
- Audit entry records termination cause.

### Compaction

A persistent agent (typically housewife) may trigger compaction of another
persistent agent, subject to `agent:compact[<target>]`. Compaction forces the
target into a session-end state: distill scratchpad to semantic, reset. Bounded
by permission scope and audit logging. Use case: housekeeping when another
agent's scratchpad has grown stale.

---

## 9. MCP Integration

MCP (Model Context Protocol) is the universal tool integration surface. All
external capabilities — email, calendar, file access, code execution, web
search, system management, knowledge bases — are exposed as MCP tool servers.

### Architecture

MCP servers run as separate processes (or remote services) and register with
Hearth's tool registry. Each server exposes a set of tools with schemas. Hearth
acts as the MCP client — the dispatcher invokes tools on behalf of agents.

### Tool injection

**v1: static injection.** At invocation time, all MCP tool schemas the agent is
permitted to use (per its `tool:invoke[...]` permissions) are resolved and
injected into the API call. This is simple and correct.

**v2 candidate: dynamic injection.** For agents with broad permissions and many
available tools, a relevance filter matches the respond-to batch against tool
descriptions and injects only top-K relevant schemas. This reduces context
bloat without changing the agent or permission model.

### Tool routing

Tools may be hosted in three locations:

1. **On the server** — built-in MCP servers (web search). Always reachable.
2. **On a client device** — localhost-bound MCP servers (file access, code
   execution, system tools). Reachable when the client daemon is connected.
3. **Remote services** — third-party MCP servers accessed over the network.

When an agent invokes a tool, the dispatcher routes the call to the appropriate
host. If the tool is on a disconnected client, the call fails — no fallback.

### Knowledge base

The knowledge base is not a custom Hearth primitive. It is an MCP tool surface.
Options:

- An existing knowledge-base solution (e.g. Obsidian + MCP adapter, or a
  dedicated RAG service) exposed as MCP tools (`kb:search`, `kb:add`,
  `kb:list`).
- A simple Hearth-hosted document store with MCP tools — if no external
  solution fits, Hearth ships a minimal one.

The key requirement: agents can read from and write to the knowledge base
through their normal tool invocation path, subject to permissions. The user can
also interact with it directly through the frontend.

### Permission integration

MCP tool invocation is gated by `tool:invoke[<tool_id>]` permissions with the
standard effect model (allow / require_confirmation / deny). An agent permitted
`tool:invoke[email:*]` with `tool:invoke[email:send]` set to
`require_confirmation` can read email freely but needs user approval to send.

---

## 10. Proactive Action System

Agents with `proactive_triggers=on` participate in a proactive loop.

### Triggers

- **Interrupt** — event-driven. External signal (time threshold, new email,
  calendar event approaching) wakes the agent.
- **Scheduled** — agent-driven. At the end of each check, the agent decides
  when to next run and writes a scheduled event. Schedules can be defined with
  cron syntax.

Both enter the agent's queue as `type: event` entries and wake the agent via
the standard lifecycle. Proactive ticks do not bypass the queue.

Both are handled by `routine_model` first. The routine model decides:

1. Does anything need to be done?
2. Can I handle it directly?
3. Should I escalate to the primary model?

### Risk tiers

- **Low** — cosmetic, reversible, local. Routine model may act directly.
- **Medium** — observable effects, messages other actors. Primary model approval
  required.
- **High** — irreversible or external side effects. User confirmation required.

Risk tier is a property of the action's permission definition, not the agent's
judgment. Dispatcher enforces it.

### Confidence gating

Each proactive decision is annotated with a confidence score. Below a
configurable threshold, the agent surfaces a suggestion instead of acting. This
is independent of risk tier.

### Context isolation

Proactive ticks run in isolated context, separate from the user-session
scratchpad. Each tick assembles fresh context (core facts + targeted retrieval
+ event payload). The scratchpad is not visible. This keeps the session
inactivity timer honest.

### Notification delivery

Proactive outputs are delivered to the user as messages in the appropriate room,
or as push notifications for high-priority items. There is no separate "inbox"
surface — the room the agent belongs to (or a default notification room) is
where proactive outputs land. The frontend surfaces unread counts and
notification badges per room.

---

## 11. Network Topology

```
┌──────────────────────────────────────────────────────┐
│ NAS / Host                                           │
│   • Hearth server (single Go binary)                 │
│   • Memory layers: SQLite files + markdown           │
│   • MCP servers: Gmail, Drive, Calendar, etc.        │
│   • Orchestration, scheduler, dispatcher             │
│   • Web frontend (Vue 3, served by Go)               │
│   • Audit log (Postgres)                             │
└──────────────────────────────────────────────────────┘
                        ▲
                        │ authenticated TCP (LAN or VPN)
                        ▼
┌──────────────────────────────────────────────────────┐
│ Client daemon (optional, stretch goal)               │
│   • Thin Hearth client                               │
│   • Local inference via Ollama                       │
│   • Localhost MCP servers: file access, code tools   │
│   • Local tool registry                              │
└──────────────────────────────────────────────────────┘
```

### Server

Single Go binary. Serves the web frontend, runs the dispatcher, manages agent
lifecycles, hosts memory, and proxies MCP tool calls.

### Client daemon (stretch goal)

Outbound websocket to server. Registers local MCP tools. Executes tool calls
locally and returns results. No open ports on the client side.

If the client is offline, its tools are unavailable — no fallback.

### Auth

Every connection is token-authenticated regardless of network. LAN is
convenient, not trusted. Tokens scoped per client, revocable from server.

---

## 12. Model Provider Registry

All model calls go through a provider registry and model mapping table.

- Agents store model strings (`claude-sonnet-4.6`, `gemma-4-local`).
- A mapping resolves that to provider + provider-specific model ID.
- Providers define protocol (`anthropic` or `openai_compatible`), base URL,
  and optional encrypted API key.
- Inference uses a single `Infer(...)` entrypoint dispatching to a protocol
  adapter.
- Streaming is first-class: adapters yield normalized chunks.
- API keys encrypted at rest, never returned by GET endpoints.

Tables: `providers`, `model_mappings`.

Seed data: defaults for Anthropic, Ollama.

---

## 13. v1 Scope and Phasing

### v1 — usable system

The minimum viable Hearth: a single-user system where you can chat with
persistent agents who remember things, use MCP tools, and manage rooms.

**Must have:**

- Actor model (user login + agent definitions).
- Persistent and ephemeral agents with feature flags.
- Memory architecture: all four layers (core facts, scratchpad, episodic,
  semantic). Distillation at session end.
- Rooms with FreeForm protocol.
- Queue, drain loop, batching, context assembly.
- Clearance (5-tier default) enforced at retrieval, send, spawn.
- Permissions (IAM-style) enforced at dispatcher.
- MCP tool integration with static schema injection.
- Model provider registry (Anthropic + Ollama).
- Web frontend: room list, room view, DMs, agent config, settings.
- Audit log.
- Single Go binary deployment.
- SQLite for everything in dev; Postgres option for audit + metadata.

**Nice to have in v1:**

- TurnTaking and FireAndForget protocols.
- Projects as structural grouping.
- Proactive tick system (scheduled + interrupt triggers).
- Basic notification delivery (in-room + badge counts).

**Deferred to v2:**

- IterativeDraft protocol.
- Custom protocol plugin interface.
- Dynamic MCP tool injection (relevance filtering).
- Client daemon.
- Knowledge base MCP server (use external solution via MCP in v1).
- Push notifications.
- Export/import room+agent bundles (the template replacement).
- Cross-clearance handoff UI (side-by-side diff review).
- Session scratchpad eviction strategies.
- Cost accounting per agent/room.

### Build order suggestion

1. **Model provider registry + inference adapter.** Get Ollama and Anthropic
   calls working. Everything else depends on this.
2. **Agent definitions + memory layers.** Core facts file, episodic writes,
   semantic store with embeddings. Test with a single agent.
3. **Rooms + FreeForm protocol + queue + dispatcher.** The messaging backbone.
   User can chat with an agent in a room.
4. **Clearance + permissions.** Enforce at the three boundary points. Add
   audit logging.
5. **MCP integration.** Register MCP servers, inject tool schemas, route
   calls. Test with a real MCP server (e.g., filesystem).
6. **Session lifecycle + distillation.** Session end triggers, scratchpad →
   semantic, checkpoint recovery.
7. **Frontend.** Room list, room view, agent config, settings. Connect via
   websocket for real-time updates.
8. **Additional protocols.** TurnTaking, FireAndForget. Test adversarial
   debate.
9. **Projects.** Grouping, summary view, child room management.
10. **Proactive system.** Scheduled ticks, risk tiers, confidence gating.

---

## 14. Open Questions

### Distillation quality

Semantic memory usefulness depends on local-model distillation quality. What
prompting strategy yields useful facts? What format — structured records,
free-form notes with embeddings, key-value? Initial approach: structured
records with embeddings, refined empirically.

### Retrieval relevance

Which embedding model? What chunking for episodic? Re-ranking? Initial
approach: nomic-embed-text, per-message chunking, semantic as discrete facts
with their own embeddings.

### Session scratchpad eviction

For agents with `session_spanning_context=on`, the scratchpad grows. How does
it stay bounded? Candidates: oldest-first eviction with retrieval fallback,
importance-weighted, soft summarization. Initial approach: oldest-first with
retrieval fallback.

### Protocol state persistence

TurnTaking and IterativeDraft have state (whose turn, iteration count). Where
does this live — in-memory (lost on crash) or persisted? Initial approach:
persisted to room metadata in the DB, reconstructed on restart.

### Debate convergence

For TurnTaking rooms used as adversarial debates: convergence detection is
hard to mechanize reliably. Initial approach: don't try. Use `max_rounds` as
the termination condition. If agents reach consensus, they can explicitly
signal it. Convergence detection is a v2 candidate, not a dispatcher
responsibility.

### Tool schema context budget

For agents with many permitted MCP tools, injecting all schemas may bloat
context. At what point? Initial approach: no filter in v1; revisit if observed.
Dynamic injection is the v2 candidate.

### Audit log retention

Grows monotonically. Retention policy? Initial approach: unbounded with
rotation to compressed archive after 90 days.

### Cost accounting

Cloud model calls cost money. Track per-agent or per-room? Surface budgets?
Initial approach: log token usage per invocation in audit; surface aggregates
in UI; budget enforcement is v2.

### Failure modes for proactive escalation

Routine model escalates to primary, but primary is unavailable (network,
rate limit, key revoked). Initial approach: log failure, demote to user
suggestion, do not act unilaterally.

---

## Appendix A — Agent Configuration Examples

The housewife — most trusted persistent agent:

```yaml
name: housewife
role_description: >
  Personal agent. Manages household, calendar, meal planning, coordinates
  other agents for projects. Highest clearance, broad permissions. Acts
  proactively on routine matters; surfaces high-risk decisions to the user.

models:
  primary: gemma-4-local
  routine: gemma-4-local
  sensitive: gemma-4-local

feature_flags:
  session_spanning_context: true
  raw_episodic_recall: true
  identity_continuity: true
  proactive_triggers: true

clearance: 5

permissions:
  - tool:invoke[*]
  - room:create
  - room:add_participant
  - room:extend_turn_limit
  - memory:retrieve[*]
  - memory:write[semantic]
  - agent:spawn[*]
  - agent:terminate
  - settings:read[*]
  - settings:write[wallpaper, theme, schedule]
  - proactive:enable
  - proactive:act[low, medium]
  - handoff:propose[*]
  - permission:grant  # condition: granted ≤ self

confidence_threshold: 0.7
```

A lower-clearance persistent agent:

```yaml
name: scout
role_description: >
  Handles external-facing tasks: web searches, API calls, public data
  retrieval. Lower clearance; sees only sanitized context. Good for tasks
  where data leaves the system.

models:
  primary: claude-sonnet-4.6
  routine: gemma-4-local
  sensitive: gemma-4-local

feature_flags:
  session_spanning_context: true
  raw_episodic_recall: false
  identity_continuity: true
  proactive_triggers: false

clearance: 2

permissions:
  - tool:invoke[web:*, api:*]
  - room:create
  - memory:retrieve[semantic]
  - memory:write[semantic]
```

An ephemeral task agent:

```yaml
name: code_review_<task_id>
role_description: >
  Review the diff at <path>. Report findings. Terminate.

models:
  primary: claude-sonnet-4.6
  routine: gemma-4-local
  sensitive: gemma-4-local

feature_flags:
  session_spanning_context: false
  raw_episodic_recall: false
  identity_continuity: false
  proactive_triggers: false

clearance: 3

permissions:
  - tool:invoke[code:read_file, code:review_diff]
  - memory:retrieve[semantic]

timeout: 600s
parent: housewife
```

---

## Appendix: Glossary

- **Actor** — any entity that participates in rooms and is subject to clearance
  and permissions. Either a user or an agent.
- **Agent** — an actor defined by configuration. Has a role, model tiers,
  feature flags, clearance, and permissions.
- **User** — an actor that authenticates with credentials. Holds implicit root
  authority.
- **Room** — a conversation container with participants, a transcript, a
  clearance ceiling, and a protocol.
- **Protocol** — a behavioral contract governing how messages flow through a
  room. Enforced by the dispatcher.
- **Project** — a structural grouping over rooms. Navigation and organization,
  not a permission boundary.
- **Clearance** — data sensitivity tier. Governs what an actor can see. Totally
  ordered integers; higher = more trust.
- **Permission** — discrete action capability. Governs what an actor can do.
- **Permission effect** — `allow`, `require_confirmation`, or `deny`.
- **Queue** — per-agent ordered buffer of incoming messages and events.
- **Respond-to batch** — same-room messages at queue head, processed in one
  invocation.
- **Ambient queue** — queue entries not in the current batch; visible as context.
- **Session** — bounded period of agent activity. 30 minutes of inactivity ends
  it.
- **Session scratchpad** — agent-writable artifact persisting across rooms
  within a session. Distilled at session end.
- **Episodic memory** — append-only raw conversation log.
- **Semantic memory** — distilled facts; per-agent; RAG-retrieved.
- **Core facts file** — small curated markdown; always injected.
- **Distillation** — extracting semantic facts from scratchpads and episodic.
- **Checkpoint** — periodic raw write for crash durability.
- **Persistent agent** — long-lived, few in number.
- **Ephemeral agent** — task-scoped, spawned and terminated.
- **Turn limit** — maximum agent-to-agent exchanges without user participation.
- **Tick** — single proactive check by an agent.
- **MCP** — Model Context Protocol. Universal tool integration surface.
- **Explicit cross-clearance handoff** — deliberate declassification of content
  for a lower-clearance recipient. User-confirmed.
- **Compaction** — forcing an agent into session-end state for housekeeping.
