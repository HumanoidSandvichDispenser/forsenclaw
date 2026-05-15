# Hearth — Design Document (v4)

A self-hosted multi-agent orchestration system with file-based agent
definitions, RFC-driven rooms, layered file-based memory, and capability-based
access control.

---

## 1. Overview

Hearth orchestrates multiple LLM agents — local and cloud — across persistent
file-based memory, RFC-driven conversation rooms, and a capability system that
separates what an actor can *see* from what an actor can *do*. It runs on a NAS
or similar always-on host.

Users and agents are both **actors** in the system. The only structural
difference: users authenticate with credentials; agents are defined by
configuration files on disk and invoked by the system via RFCs. Both participate
in rooms, send messages, hold permissions, and are subject to clearance.

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
- RFC-driven rooms where protocols actively orchestrate agent invocation rather
  than passively gating sends.
- File-based agent definitions and memory, XDG-compliant.
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
  their memory files are continuous; the active context window is bounded and
  assembled per-invocation.
- **Defense in depth at the boundary, not the storage.** Memory is stored at
  full fidelity; clearance is enforced at retrieval, message send, and context
  injection — not by deleting or summarizing data on disk.
- **Stateless models, stateful agents.** Models are compute primitives. Agents
  are the persistent identity that owns context, memory access, and permissions.
- **No ambient authority.** Every privileged action — tool dispatch, room
  creation, memory write, config change — is gated by an explicit permission
  grant.
- **Trust is at the agent, not the model.** An agent's clearance determines
  what data it may see. Within that clearance, the agent may route calls to any
  of its configured models.
- **Protocols orchestrate; agents respond.** Protocols actively issue RFCs to
  agents. Agents do not decide when to speak — the protocol does. New
  interaction patterns are new protocols, not new primitives.
- **Files over databases for identity and knowledge.** Agent definitions and
  memory are files on disk — readable, editable, version-controllable without a
  running server. Databases are for queryable runtime state and audit.

### Tech stack

- **Backend:** Go
  - Concurrency: goroutines + channels.
  - Database driver: standard `database/sql` with modernc.org/sqlite (pure-Go
    SQLite, no CGO).
  - Migrations: goose or golang-migrate.
  - Scheduling: time.Ticker + priority queue for proactive ticks.
- **Frontend:** Vue 3 + TypeScript + Pinia
- **Storage:**
  - Agent definitions: YAML files on disk.
  - Agent memory: Markdown files on disk.
  - Room transcripts: JSONL files on disk.
  - Room metadata, protocol state, audit log: SQLite.
  - Search index: SQLite (cache-tier, rebuildable).
- **Local inference:** Ollama over HTTP (not in-process).

---

## 2. Actor Model

An **actor** is any entity that participates in rooms, sends messages, and is
subject to clearance and permissions. There are two kinds:

### Users

A user authenticates with credentials (v1: username + password; later: hardware
token, biometric). On authentication, the user receives a session token scoped
to the connection. The user is the **root identity** — they can do anything:
spawn agents, modify settings, grant or revoke permissions, read any audit log,
terminate the server, read or write any memory layer or config file. Root access
is identity, not a permission grant.

### Agents

An agent is defined by a YAML configuration file on disk and invoked by the
system via RFCs. An agent has a role, model assignments, feature flags,
clearance, and permissions. Agents do not authenticate — they are instantiated
by the server from their file definitions.

### What they share

Both users and agents:

- Participate in rooms as message senders and recipients.
- Are subject to room protocols (RFC ordering, turn limits).
- Have a clearance tier that governs data visibility.
- Have permissions that govern actions (though users hold implicit root).
- Appear in room transcripts with their identity as the sender.

### What differs

| Concern | User | Agent |
|---|---|---|
| Authentication | Credentials | Definition file + RFC invocation |
| Authority | Root (implicit) | Granted permissions |
| Memory ownership | N/A (user is the subject of memory) | Per-agent files (MEMORY.md + daily notes) |
| Clearance | Implicitly top-tier | Configured per-agent |
| Lifecycle | Login/logout | Persistent or ephemeral |
| Invocation | Self-directed (types when they want) | Protocol issues RFC |

This unification means the frontend treats DMs with agents and multi-participant
rooms identically. A DM with the housewife is just a room with two
participants. A structured debate room with three agents is the same primitive
with a different protocol.

---

## 3. Agent Model

An **agent** is a persistent identity defined by:

1. A **definition file** (`agent.yaml`) — role, system prompt, behavioral
   configuration, model assignments, feature flags, clearance, permissions.
2. A **memory directory** — `MEMORY.md` for curated long-term memory,
   `memory/*.md` for daily working notes.

The agent owns its context window assembly, its memory files, its permission
set, and its identity across sessions. Models are stateless compute that the
agent invokes.

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
| `identity_continuity` | MEMORY.md persists across sessions | Memory starts fresh each session |
| `daily_notes` | Daily note files maintained, indexed for search | No daily notes; context from MEMORY.md only |
| `proactive_triggers` | Runs scheduled and interrupt-driven proactive ticks | Acts only when invoked via RFC |
| `dreaming` | Background consolidation from daily notes → MEMORY.md | No automatic promotion; manual curation only |

Companion-style agents typically have all four on. Task-style agents typically
have all four off.

### Context assembly

Assembled fresh each invocation (per RFC). Order:

1. **System prompt** — from `agent.yaml` role description.
2. **MEMORY.md** — always injected. The agent's curated long-term memory.
3. **Daily notes** — today's and yesterday's `memory/YYYY-MM-DD.md`, if
   `daily_notes=on`.
4. **RAG retrieval** — search index queried against the RFC payload. Retrieves
   relevant chunks from older daily notes and MEMORY.md.
5. **MCP tool schemas** — the agent's permitted tools, resolved and injected
   (§9).
6. **Room history** — target room's recent transcript, up to configured window.
7. **Turn budget notice** — remaining turns before user approval required.
8. **RFC payload** — the actual request the agent is responding to, including
   any pending interjections.

### Agents are not knowledge specialists

LLMs already have broad knowledge. The role of a persistent agent is not "knows
about cooking" or "knows about code" — it is defined by **what it knows about
the user** (clearance), **what it can do** (permissions), and **what it's
responsible for** (role). The housewife is the most trusted agent not because it
knows the most about the world, but because it knows the most about *you*. A
lower-clearance agent isn't dumber — it has less context about your life.

---

## 4. Memory Architecture

Hearth's memory is **files on disk** that agents read and write. The model only
"remembers" what is saved to files — there is no hidden state. This design is
inspired by OpenClaw's memory model.

Memory is **owned by agents, not rooms**. Each persistent agent has its own
memory directory under `$XDG_DATA_HOME/hearth/agents/<name>/`.

### MEMORY.md — long-term curated memory

A markdown file that is always injected at the start of any context assembly.
Contains durable facts, preferences, standing decisions, and concise summaries.
Functions like a project document: high-signal, low-volume, stable.

MEMORY.md is the floor of an agent's knowledge — user identity, persistent
project state, the agent's own role and constraints, key preferences. Agents
write to MEMORY.md freely (it is data, not config — no approval flow required).

The user can also edit MEMORY.md directly with any text editor.

If MEMORY.md grows past a configured budget, Hearth truncates the injected copy
(not the file on disk). This signals the agent (or user) to move detailed
material into daily notes and keep only durable summaries in MEMORY.md.

### Daily notes — working memory

Per-day markdown files at `memory/YYYY-MM-DD.md`. Running context, observations,
session summaries, detailed notes. Today's and yesterday's notes are loaded
automatically into context. Older notes are indexed for search retrieval.

Daily notes are the working layer. Agents write to them during sessions —
capturing observations, decisions, reasoning that may be useful later but isn't
yet curated into MEMORY.md.

### Dreaming — background consolidation

For agents with `dreaming=on`, a background pass periodically reviews daily
notes and promotes durable material into MEMORY.md. This runs on the
`routine_model` to keep costs low.

Dreaming is not automatic promotion — it is a consolidation pass that the agent
(via routine model) performs with judgment about what is durable vs. transient.
Promoted content is written to MEMORY.md; the daily notes are not deleted (they
remain searchable).

The user can review dreaming activity. Initial approach: dreaming writes a brief
summary of what it promoted and why to a `DREAMS.md` file for human review.

### Search index

A SQLite-backed hybrid search index (vector similarity + keyword matching) over
MEMORY.md and all daily notes. Embeddings computed locally (Ollama + embedding
model).

The search index is infrastructure, not a memory layer. It is rebuildable from
the files on disk at any time and lives in `$XDG_CACHE_HOME/hearth/`.

Retrieval is parameterized by the requesting agent's clearance tier. An agent
can search its own memory freely. Cross-agent memory search is possible where
clearance permits, but the default is agent-scoped.

### Sessions and lifecycle

A **session** is a bounded period of agent activity.

- Ends after **30 minutes of inactivity** on a per-agent basis.
- Within a session: agents write observations to today's daily note.
- At session end: if `dreaming=on`, a consolidation pass is scheduled (does not
  block).
- Proactive actions run in isolated session contexts — they do not extend or
  contaminate user-facing sessions.

### Room transcripts — shared conversation history

Room transcripts are stored as JSONL files at
`$XDG_DATA_HOME/hearth/rooms/<room-id>.jsonl`. Each line is a message record:

```json
{
  "id": "msg_001",
  "timestamp": "2026-05-14T10:30:00Z",
  "room": "room_abc",
  "sender": "housewife",
  "clearance_tag": 5,
  "type": "message",
  "content": "..."
}
```

Transcripts are append-only. They are the shared record of what happened in a
room — distinct from agent memory, which is an agent's personal understanding
of what matters.

Room transcripts are indexed by the search system for cross-room retrieval
where clearance permits.

---

## 5. Rooms and Protocols

Rooms and protocols are separate concepts. A **room** is a transcript container
with participants. A **protocol** is a behavioral contract that governs how and
when agents are invoked via RFCs. Rooms are infrastructure; protocols are
orchestration policy.

### Rooms

A room has:

- A **participant list** (actors — users and/or agents).
- A **transcript file** (JSONL on disk).
- A **clearance ceiling** — the highest clearance tier any message may carry.
- A **protocol** — the behavioral contract governing the room.
- A **parent** (optional) — for structural grouping under a project.

Room metadata (participant list, clearance ceiling, protocol type and
configuration, protocol state) is stored in SQLite for queryability. The
transcript itself is the JSONL file.

### Room creation

Any actor with the `room:create` permission may create a room. The creator
selects initial participants, clearance ceiling, and protocol configuration.

A participant whose clearance is below the room's ceiling cannot join. Adding a
participant whose clearance is below existing message classifications is not
permitted — the resolution is to create a new room with an explicit
cross-clearance handoff to seed it (§7).

### Protocols and RFCs

A **protocol** is a named behavioral contract that actively orchestrates agent
invocation. The fundamental primitive is the **RFC** (Request for Comment) — a
protocol-issued request for an agent to produce a response.

Agents do not decide when to speak. The protocol decides. An agent is invoked
only when it receives an RFC. This is the central architectural difference from
a queue-and-validate model: instead of agents waking on every message and
checking whether they're allowed to respond, the protocol wakes agents
precisely when their response is needed.

#### RFC structure

```go
type RFC struct {
    ID        string
    Room      string    // which room this is for
    Target    string    // agent name (or "*" for broadcast)
    Payload   RFCPayload
    Deadline  time.Time // optional timeout
}

type RFCPayload struct {
    // Messages the agent should respond to — the "task"
    Messages     []Message
    // Interjections that arrived while another agent was responding
    Interjections []Message
    // Protocol-specific metadata (iteration count, round number, etc.)
    Metadata     map[string]any
}
```

#### Protocol interface

```go
type Protocol interface {
    // Start is called when the room is created or the protocol is activated.
    // The protocol begins issuing RFCs according to its logic.
    Start(room *Room, dispatcher Dispatcher)

    // OnMessage is called when a message lands in the room (from a user
    // or from an agent responding to an RFC). The protocol decides what
    // to do next: issue another RFC, terminate, wait, etc.
    OnMessage(room *Room, sender Actor, msg Message)

    // OnRFCResponse is called when an agent completes its RFC response.
    // The protocol may issue the next RFC, collect for broadcast
    // reconciliation, or terminate.
    OnRFCResponse(room *Room, rfc RFC, response Message)

    // OnInterjection is called when a non-targeted actor sends a message
    // (e.g., user typing during a debate). The protocol decides whether
    // to queue it for the next RFC, pause, or ignore.
    OnInterjection(room *Room, sender Actor, msg Message)

    // ShouldTerminate returns true if the protocol's end condition is met.
    ShouldTerminate(room *Room) bool

    // State returns serializable protocol state for persistence.
    State() ProtocolState

    // Restore reconstructs protocol state from persisted data.
    Restore(state ProtocolState)
}
```

The `Dispatcher` interface passed to `Start` and available to the protocol is
how it issues RFCs:

```go
type Dispatcher interface {
    // IssueRFC sends an RFC to a specific agent.
    IssueRFC(rfc RFC) error

    // BroadcastRFC sends an RFC to all agent participants simultaneously.
    BroadcastRFC(room *Room, payload RFCPayload) error
}
```

### Built-in protocols

**FreeForm** — for two-participant rooms only. When participant A sends a
message, the protocol issues an RFC to participant B. Suitable for DMs and pair
conversations (user + agent, or agent + agent).

Freeform rooms are limited to exactly two participants. Multi-agent
conversations require a structured protocol.

Configuration:
- `max_turns`: hard limit on agent-to-agent exchanges without user
  participation (safety mechanism for agent + agent freeform rooms).

**RoundRobin** — participants speak in a defined order. The protocol issues an
RFC to each participant sequentially, waiting for each response before issuing
the next. Configurable:
- `turn_order`: list of participant names.
- `max_rounds`: hard limit on full rotations.
- `include_user`: whether the user gets a turn slot or can only interject.

This is the protocol that enables structured debate. Two agents with opposing
system prompts in a RoundRobin room *is* the debate feature — no special room
type needed.

**Broadcast** — all agent participants receive simultaneous RFCs and respond
independently. The protocol collects all responses before proceeding to the
next round. Configurable:
- `max_rounds`: hard limit on broadcast rounds.
- `reconciliation`: whether a designated agent synthesizes after collection.
- `reconciler`: which agent reconciles (if enabled).

This is the protocol for planning poker, independent estimation, parallel
review — any workflow where agents should respond without seeing each other's
answers first.

**IterativeDraft** — collaborative creation with user in the loop. Operates in
cycles: the protocol RFCs the drafter, presents the draft to the user, waits
for user feedback, then RFCs the drafter again with the feedback. Configurable:
- `max_iterations`: hard limit on draft cycles.
- `drafter`: which agent drafts.
- `auto_close_on_accept`: whether the room closes when the user accepts.

**FireAndForget** — task delegation. One actor sends a task; the protocol
issues a single RFC to the executor agent. The room closes on response delivery
or timeout. Configurable:
- `timeout`: duration before forced close.
- `result_to`: where the result is forwarded (a room, or the spawning actor's
  DM).

### User messages and interjections

Users are not invoked via RFC — they send messages whenever they choose. The
protocol handles user messages through its `OnMessage` and `OnInterjection`
methods:

- **In FreeForm rooms:** A user message triggers an RFC to the other
  participant. This is the normal flow.
- **In RoundRobin/Broadcast rooms:** A user message while the protocol is
  awaiting an agent's RFC response is queued as an interjection. The
  interjection is included in the current or next RFC payload so the agent sees
  it. If the user has a turn slot (`include_user=true`), their turn is handled
  by the protocol waiting for a user message instead of issuing an RFC.
- **In IterativeDraft rooms:** User messages are the feedback input the
  protocol waits for between draft cycles.

If an agent is mid-response when an interjection arrives (analogous to typing
while a coding harness is running), the interjection is queued until the
agent's current response completes, then included in the next RFC.

### Turn limits (cross-protocol)

Regardless of protocol, all rooms enforce a turn limit on agent-to-agent
exchanges without user participation. This is a safety mechanism, not a
protocol feature.

- A **turn** is one completed RFC response.
- Turn limits are per-room.
- Turn limits apply only when no user has sent a message recently. A user
  message resets the counter.
- Each room has configurable soft and hard limits.

When the hard limit is reached, the protocol stops issuing RFCs. To continue,
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
- An optional set of associated agents (persistent agents relevant to the
  project's domain).
- A clearance ceiling inherited by child rooms unless overridden.
- A summary view that surfaces major events from child rooms.

Projects are navigation and organizational primitives, not permission
boundaries.

### Composing interaction patterns

The protocol model means you don't need named room types to get complex
behavior. Examples:

- **Adversarial debate:** RoundRobin room, two agents with opposing prompts,
  `max_rounds=10`. User observes or interjects.
- **Planning poker:** Broadcast room, N agents estimating independently, then
  a reconciliation round by a designated reconciler.
- **Document workshop:** IterativeDraft room, one drafter agent, user as
  feedback source.
- **Task delegation:** FireAndForget room, orchestrator sends task, ephemeral
  agent executes.
- **DM chat:** FreeForm room, user + agent.
- **Agent pair conversation:** FreeForm room, agent + agent, with turn limit
  as safety.

New patterns emerge from new protocol implementations, not new primitives.

---

## 6. Agent Invocation

### The RFC as universal invocation primitive

An agent is invoked **only** via RFC. This is the single entry point for all
agent computation — room messages, proactive triggers, and system events all
flow through the same mechanism.

RFC sources:

1. **Room protocols** — the primary source. Protocol issues an RFC when it's
   the agent's turn to respond.
2. **Proactive system** — issues system RFCs for scheduled ticks and
   interrupt-driven events (§10).
3. **Config proposals** — the system issues an RFC when an agent requests a
   config change and confirmation is needed.

### Agent lifecycle (awake/asleep)

An agent is either **awake** (goroutine running, processing an RFC) or
**asleep** (no goroutine).

- Wakes when an RFC arrives.
- Processes the RFC: assembles context, invokes the model, produces a response.
- Sleeps when the RFC is complete and no further RFCs are pending.

Implementation: per-agent goroutine spawned on RFC receipt. If the agent is
already processing an RFC, additional RFCs are queued (per-agent, ordered).
The goroutine drains the RFC queue and sleeps when empty.

### RFC processing

When an agent receives an RFC:

1. **Assemble context** (§3 — context assembly order).
2. **Invoke the model** on the appropriate tier.
3. **Post-process the response** — extract tool calls, memory writes, config
   proposals.
4. **Execute tool calls** if permitted (subject to permissions, §7).
5. **Write memory** — agent may update today's daily note or MEMORY.md.
6. **Deliver the response** — message goes to the room transcript. The
   protocol's `OnRFCResponse` is called.
7. **Complete the RFC** — agent moves to next queued RFC or sleeps.

### Pending interjections

When a user (or another actor) sends a message to a room while an agent is
mid-response to an RFC, the message is held as a pending interjection. Once
the agent's current RFC completes:

- The protocol's `OnInterjection` is called with the pending message.
- The protocol decides whether to include it in the next RFC, issue a new
  RFC, or take other action.

This mirrors the behavior of coding harnesses where user input during agent
execution is queued until the current step completes.

### SLM harness considerations

The invocation pipeline is the correct layer for small-model scaffolding.
Techniques for SLMs (per-turn skill injection, output repair for malformed tool
calls, quality monitoring for empty/looping responses, thinking-budget caps)
apply at the RFC processing level, transparent to the agent definition and
protocol.

---

## 7. Access Control

Clearance and permissions are **orthogonal** axes with distinct enforcement
points.

### Clearance — what an actor can see

Clearance is a tiered data classification. Tiers form a totally ordered set of
integers; higher numbers mean more trust and more sensitive data. The number of
tiers and their meaning is user-configured.

Default five-tier scheme:

- **Tier 5** — full access. User and most-trusted agent. Sees everything.
- **Tier 4** — broad access with sensitive-data exclusions (credentials,
  financial, etc).
- **Tier 3** — task-scoped. May see specific projects or topics, not general
  life context.
- **Tier 2** — public-only. Sanitized inputs. Suitable for agents hitting
  external APIs.
- **Tier 1** — minimum trust. Should never see user data.

Users can add, remove, renumber, or rename tiers.

Clearance is enforced at three points:

1. **Retrieval** — every search query filters by requesting actor's tier.
2. **Outgoing message send** — dispatcher checks message classification against
   destination room ceiling. Rejected if above.
3. **Spawn-time context injection** — context passed to a child agent must not
   exceed the child's clearance. Dispatcher rejects violating spawns.

Classification at write time: a message sent in a tier-3 room is tagged tier-3
in the transcript and never appears in a tier-2 retrieval. Default
classification is the sender's clearance, capped at the room's ceiling.

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

### Permissions — what an actor can do

Permissions are fine-grained action capabilities, IAM-style. Each statement
has:

- **Action** — the gated operation (e.g. `config:write[self]`).
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
- `memory:write[<layer>]`, `memory:search[<scope>]` — write to own memory
  files, search across agents.
- `agent:spawn[<role>]`, `agent:terminate`, `agent:compact[<target>]`.
- `config:read[<scope>]`, `config:write[<scope>]` — read/write agent or server
  configuration files. Scopes: `self` (own agent.yaml), `server`
  (hearth.yaml), `agent:<name>` (another agent's definition).
- `proactive:enable`, `proactive:act[<risk_tier>]`.
- `handoff:propose[<target_clearance>]`.
- `audit:read`.

**Default effects for config permissions:**

| Permission | Default effect |
|---|---|
| `config:read[self]` | `allow` |
| `config:write[self]` | `require_confirmation` |
| `config:read[server]` | `require_confirmation` |
| `config:write[server]` | `require_confirmation` |
| `config:read[agent:*]` | `deny` |
| `config:write[agent:*]` | `deny` |

These defaults mean an agent can read its own definition freely, but any
modification — to itself, to the server, or to other agents — requires user
approval unless explicitly granted `allow`. Reading server config also requires
approval by default, since provider definitions and model mappings may contain
sensitive information about the deployment.

### Config staging and approval

When an agent proposes a config change (writing to any file under
`$XDG_CONFIG_HOME/hearth/`), the flow is:

1. Agent writes the proposed new file to the staging area at
   `$XDG_DATA_HOME/hearth/staging/agents/<name>/agent.yaml.proposed`.
2. If the permission effect is `require_confirmation`: the dispatcher computes
   a diff and surfaces it to the user for review. The user approves, edits, or
   rejects.
3. If the permission effect is `allow`: the system applies the change directly
   (copies proposed file to config path).
4. If the permission effect is `deny`: the agent cannot propose the change.

On approval, the proposed file replaces the config file. On rejection or
expiry, the proposed file is deleted. The audit log records the proposal, the
diff, and the outcome.

One pending proposal per agent per config file. A new proposal overwrites the
previous one.

This generalizes beyond agent definitions — if an agent wants to modify
`hearth.yaml` (e.g., adding a model provider), same flow with
`config:write[server]`.

### Audit

The audit log records **actions taken** and **attempts denied**:

- Tool invocations (call + arguments + result status).
- Permission denials.
- `require_confirmation` checks (request + user response).
- Config proposals (diff + outcome).
- Agent spawn and teardown (ephemeral agent definitions snapshotted here).
- Cross-clearance rejections.
- Handoff proposals and resolutions.

Append-only, SQLite-backed, accessible to the user without restriction.
`audit:read` is its own permission for agents; they don't get it by default.

---

## 8. Agent Lifecycle

### Persistent agents

Long-lived. Defined by files on disk, exist indefinitely. Stable identity,
accumulated memory, long-term permission grants. The user typically has 1–3.

A persistent agent has:
- A definition file at `$XDG_CONFIG_HOME/hearth/agents/<name>/agent.yaml`.
- A memory directory at `$XDG_DATA_HOME/hearth/agents/<name>/`.

The housewife is the default persistent agent — highest clearance, broadest
permissions, most accumulated context about the user. It is not an
"orchestrator" architecturally — it's just the most trusted agent, and it's
good at orchestration as a consequence of knowing the most. Users are not
required to have an orchestrator; they can talk directly to any agent.

Persistent agents may not self-terminate without user confirmation — loss of
accumulated memory context is irreversible.

### Ephemeral agents

Spawned for a task, given scoped context, terminated on completion. Ephemeral
agents have **no files on disk** — no definition file, no memory directory.
They exist as in-memory objects with a captured configuration.

Characterized by:

- Spawned by a persistent agent or the user.
- Inherit a subset of the spawner's permissions, never more.
- Clearance tier equal to or lower than the spawner's.
- Scoped context injection at spawn (not a full memory dump).
- Do not accumulate memory — no MEMORY.md, no daily notes.
- Discarded after task completion, timeout, room closure, or revocation.
- Definition snapshotted into the audit log at spawn time so references are
  never broken.

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

- Room transcript already has the full conversation (JSONL file).
- Definition snapshot already in audit log (from spawn).
- In-memory agent state is discarded.
- Audit entry records termination cause.

### Compaction

A persistent agent (typically housewife) may trigger compaction of another
persistent agent, subject to `agent:compact[<target>]`. Compaction forces a
dreaming pass: consolidate daily notes into MEMORY.md, clean up stale entries.
Bounded by permission scope and audit logging.

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
injected into the API call. Simple and correct.

**v2 candidate: dynamic injection.** For agents with broad permissions and many
available tools, a relevance filter matches the RFC payload against tool
descriptions and injects only top-K relevant schemas. Reduces context bloat
without changing the agent or permission model.

### Tool routing

Tools may be hosted in two locations:

1. **On the server** — built-in MCP servers (web search, system tools). Always
   reachable.
2. **Remote services** — third-party MCP servers accessed over the network.

If a tool's host is unreachable, the call fails — no fallback.

### Knowledge base

The knowledge base is an MCP tool surface, not a custom Hearth primitive.
Options:

- An existing knowledge-base solution (Obsidian + MCP adapter, dedicated RAG
  service) exposed as MCP tools.
- A simple Hearth-hosted document store with MCP tools — if no external
  solution fits.

Agents interact with the knowledge base through normal tool invocation, subject
to permissions.

### Permission integration

MCP tool invocation is gated by `tool:invoke[<tool_id>]` permissions with the
standard effect model. An agent permitted `tool:invoke[email:*]` with
`tool:invoke[email:send]` set to `require_confirmation` can read email freely
but needs user approval to send.

---

## 10. Proactive Action System

Agents with `proactive_triggers=on` participate in a proactive loop. The
proactive system uses RFCs as its invocation mechanism, unifying the agent
invocation path.

### Triggers

- **Interrupt** — event-driven. External signal (time threshold, new email,
  calendar event approaching) generates a system RFC to the agent.
- **Scheduled** — agent-driven. At the end of each check, the agent decides
  when to next run and writes a scheduled event. Schedules can be defined with
  cron syntax. When the schedule fires, the system issues an RFC.

Both are delivered as system RFCs — not room-scoped, but carrying the trigger
payload. The agent processes them through the same invocation pipeline as room
RFCs, but with isolated context (no room history, no room transcript — just
MEMORY.md + daily notes + trigger payload + targeted retrieval).

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

Proactive RFCs run in isolated context, separate from any active room session.
Each proactive invocation assembles fresh context (MEMORY.md + targeted
retrieval + trigger payload). This keeps session inactivity timers honest —
proactive ticks don't extend user-facing sessions.

### Notification delivery

Proactive outputs are delivered as messages in the appropriate room, or as
system notifications for high-priority items. There is no separate "inbox"
surface — the room the agent belongs to (or a default notification room) is
where proactive outputs land. The frontend surfaces unread counts and
notification badges per room.

---

## 11. Storage Architecture

Hearth follows the XDG Base Directory Specification. All paths respect
environment variable overrides (`$XDG_CONFIG_HOME`, `$XDG_DATA_HOME`,
`$XDG_CACHE_HOME`).

### Directory layout

```
$XDG_CONFIG_HOME/hearth/                  # Config — small, version-controllable
├── hearth.yaml                           # Server config: providers, models, listen addr
└── agents/
    ├── housewife/
    │   └── agent.yaml                    # Role, models, flags, permissions, clearance
    └── scout/
        └── agent.yaml

$XDG_DATA_HOME/hearth/                    # Data — large, persistent
├── agents/
│   ├── housewife/
│   │   ├── MEMORY.md                     # Curated long-term memory
│   │   ├── DREAMS.md                     # Dreaming activity log (optional)
│   │   └── memory/
│   │       ├── 2026-05-12.md
│   │       └── 2026-05-13.md
│   └── scout/
│       ├── MEMORY.md
│       └── memory/
├── rooms/
│   ├── <room-id>.jsonl                   # Room transcripts
│   └── ...
├── staging/                              # Pending config proposals
│   └── agents/
│       └── housewife/
│           └── agent.yaml.proposed
└── db/
    ├── rooms.db                          # Room metadata, protocol state
    └── audit.db                          # Audit log

$XDG_CACHE_HOME/hearth/                   # Cache — rebuildable
├── search.db                             # Hybrid search index
└── embeddings/                           # Cached embedding vectors
```

### What lives where and why

| Data | Location | Rationale |
|---|---|---|
| Agent definitions | Config | Identity. Version-controllable. Mountable in Docker. |
| Server config | Config | Providers, models, listen address. Rarely changes. |
| Agent memory | Data | Agent-written, grows over time, persistent. |
| Room transcripts | Data | Append-only conversation records. |
| Config proposals | Data | Staging area for pending changes. Not yet config. |
| Room metadata | Data (SQLite) | Dynamic, queryable (list rooms, filter by participant). |
| Audit log | Data (SQLite) | Structured queries, append-only guarantees. |
| Search index | Cache | Rebuildable from files. Excludable from backups. |
| Embeddings | Cache | Rebuildable. Expensive to recompute but not critical. |

### Version control

`$XDG_CONFIG_HOME/hearth/` is can be configured as a git repo with agent
definitions + server config. `$XDG_DATA_HOME/hearth/agents/` is optionally a
second repo for memory history. Room transcripts can be included or excluded;
they're append-only so git handles them, but they grow.

The `db/` directory is excluded from version control. SQLite databases are
queryable runtime state; audit can be exported if archival is needed.

### Docker volume mapping

```yaml
volumes:
  - hearth_config:/home/user/.config/hearth    # Small, version-controllable
  - hearth_data:/home/user/.local/share/hearth  # Large, persistent
  # cache intentionally omitted — rebuilt on start
```

Config volume can be mounted read-only to prevent agent self-modification
entirely (overriding any `config:write` permissions). This is a deployment-level
decision.

---

## 12. Network Topology

```
┌──────────────────────────────────────────────────────┐
│ NAS / Host                                           │
│   • Hearth server (single Go binary)                 │
│   • Agent files: definitions + memory (XDG paths)    │
│   • MCP servers: Gmail, Drive, Calendar, etc.        │
│   • Orchestration, scheduler, dispatcher             │
│   • Web frontend (Vue 3, served by Go)               │
│   • SQLite databases: rooms, audit, search           │
└──────────────────────────────────────────────────────┘
                        ▲
                        │ authenticated TCP (LAN or VPN)
                        ▼
┌──────────────────────────────────────────────────────┐
│ User devices                                         │
│   • Web browser → frontend                           │
│   • (Stretch: thin client daemon for local tools)    │
└──────────────────────────────────────────────────────┘
```

### Server

Single Go binary. Serves the web frontend, runs the dispatcher, manages agent
lifecycles, hosts memory, and proxies MCP tool calls. All file I/O is to the
XDG directories.

### Auth

Every connection is token-authenticated regardless of network. LAN is
convenient, not trusted. Tokens scoped per client, revocable from server.

---

## 13. Model Provider Registry

All model calls go through a provider registry defined in `hearth.yaml`.

```yaml
providers:
  - name: anthropic
    protocol: anthropic
    base_url: https://api.anthropic.com
    api_key_env: ANTHROPIC_API_KEY    # resolved from environment

  - name: ollama
    protocol: openai_compatible
    base_url: http://ollama-docker-container-name:11434

models:
  claude-sonnet-4.6:
    provider: anthropic
    provider_model: claude-sonnet-4-20250514

  gemma-4-local:
    provider: ollama
    provider_model: gemma3:12b
```

- Agents reference model strings (`claude-sonnet-4.6`, `gemma-4-local`).
- The registry resolves to provider + provider-specific model ID.
- Inference uses a single `Infer(...)` entrypoint dispatching to a protocol
  adapter.
- Streaming is first-class: adapters yield normalized chunks.
- API keys resolved from environment variables, never stored in config files.

---

## 14. v1 Scope and Phasing

### v1 — usable system

The minimum viable Hearth: a single-user system where you can chat with
persistent agents who remember things, use MCP tools, and manage rooms.

**Must have:**

- Actor model (user login + file-based agent definitions).
- Persistent and ephemeral agents with feature flags.
- Memory architecture: MEMORY.md + daily notes + search index. Dreaming pass.
- Rooms with FreeForm protocol (2 participants).
- RFC-based invocation, dispatcher, context assembly.
- Clearance (5-tier default) enforced at retrieval, send, spawn.
- Permissions (IAM-style) enforced at dispatcher, including config read/write
  with staging and approval.
- MCP tool integration with static schema injection.
- Model provider registry (Anthropic + Ollama) in `hearth.yaml`.
- Room metadata and audit in SQLite. Transcripts as JSONL.
- Web frontend: room list, room view, DMs, agent config viewer, settings.
- Audit log.
- Single Go binary deployment.
- XDG-compliant storage layout.

**Nice to have in v1:**

- RoundRobin and FireAndForget protocols.
- Broadcast protocol.
- Projects as structural grouping.
- Proactive tick system via system RFCs.
- Basic notification delivery (in-room + badge counts).
- Config diff review UI.

**Deferred to v2:**

- IterativeDraft protocol.
- Custom protocol plugin interface.
- Dynamic MCP tool injection (relevance filtering).
- Client daemon for local tools.
- Knowledge base MCP server.
- Push notifications.
- Cross-clearance handoff UI (side-by-side diff review).
- Cost accounting per agent/room.
- Memory budget management (auto-truncation of MEMORY.md).
- Export/import room + agent bundles.

### Build order

1. **Storage layout + server config.** Establish XDG paths, `hearth.yaml`
   parsing, agent definition loading from disk.
2. **Model provider registry + inference adapter.** Get Ollama and Anthropic
   calls working. Everything else depends on this.
3. **Agent definitions + memory files.** MEMORY.md, daily notes, search index.
   Test with a single agent reading and writing its own memory.
4. **Rooms + FreeForm protocol + RFC dispatcher.** The messaging backbone.
   Room metadata in SQLite, transcripts as JSONL. User can chat with an agent
   in a room.
5. **Clearance + permissions + config staging.** Enforce at the three boundary
   points. Add audit logging. Implement config proposal and approval flow.
6. **MCP integration.** Register MCP servers, inject tool schemas, route calls.
   Test with a real MCP server (e.g., filesystem).
7. **Session lifecycle + dreaming.** Session end triggers, dreaming pass from
   daily notes → MEMORY.md.
8. **Frontend.** Room list, room view, agent config, settings, config diff
   review. Connect via websocket for real-time updates.
9. **Additional protocols.** RoundRobin, Broadcast, FireAndForget. Test
   structured debate.
10. **Projects.** Grouping, summary view, child room management.
11. **Proactive system.** Scheduled ticks via system RFCs, risk tiers,
    confidence gating.

---

## 15. Open Questions

### Search index location

Should the search index live in `$XDG_CACHE_HOME` (rebuildable, excluded from
backups) or `$XDG_DATA_HOME/db/` (persistent)? Rebuilding the index requires
re-embedding all memory files, which costs time and compute. If the embedding
model changes, the index must be rebuilt anyway. Initial approach: cache, with
a rebuild command (`hearth index --rebuild`).

### Dreaming quality

Dreaming effectiveness depends on the routine model's ability to judge what's
durable vs. transient. What prompting strategy yields useful promotions? Initial
approach: structured prompt asking for facts, preferences, and decisions that
would be useful "next month." Refined empirically.

### Retrieval relevance

Which embedding model? What chunking for daily notes? Re-ranking? Initial
approach: nomic-embed-text via Ollama, per-paragraph chunking, hybrid
keyword + vector search.

### MEMORY.md budget

How large before truncation? What's the right budget relative to context window
size? Initial approach: configurable per-agent, default 4K tokens. Truncation
keeps the top of the file (most curated content tends to be organized
top-down).

### Protocol state persistence

RoundRobin and Broadcast have state (whose turn, round count, collected
responses). This lives in `rooms.db` and is reconstructed on server restart.

### Freeform agent-agent termination

For FreeForm rooms with two agents: beyond the turn limit, how do agents signal
"we're done"? Initial approach: don't try to detect convergence. Use
`max_turns` as the hard stop. Agents can explicitly signal completion by
including a structured marker in their response.

### Transcript growth

JSONL transcript files grow indefinitely for long-lived rooms. Rotation?
Archival? Initial approach: unbounded. Revisit if observed as a problem.
Transcripts compress well.

### Cost accounting

Cloud model calls cost money. Track per-agent or per-room? Surface budgets?
Initial approach: log token usage per invocation in audit; surface aggregates
in UI; budget enforcement is v2.

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
  identity_continuity: true
  daily_notes: true
  proactive_triggers: true
  dreaming: true

clearance: 5

permissions:
  - tool:invoke[*]
  - room:create
  - room:add_participant
  - room:extend_turn_limit
  - memory:write[*]
  - memory:search[*]
  - agent:spawn[*]
  - agent:terminate
  - agent:compact[*]
  - config:read[self]                          # allow (default)
  - config:write[self]:require_confirmation    # can modify own definition
  - config:read[server]:require_confirmation   # can read server config
  - config:write[server]:require_confirmation  # can propose server changes
  - proactive:enable
  - proactive:act[low, medium]
  - handoff:propose[*]
  - permission:grant  # condition: granted ≤ self
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
  identity_continuity: true
  daily_notes: true
  proactive_triggers: false
  dreaming: true

clearance: 2

permissions:
  - tool:invoke[web:*, api:*]
  - room:create
  - memory:write[self]
  - memory:search[self]
  - config:read[self]
```

An ephemeral task agent (in-memory only, snapshotted to audit):

```yaml
name: code_review_<task_id>
role_description: >
  Review the diff at <path>. Report findings. Terminate.

models:
  primary: claude-sonnet-4.6
  routine: gemma-4-local
  sensitive: gemma-4-local

feature_flags:
  identity_continuity: false
  daily_notes: false
  proactive_triggers: false
  dreaming: false

clearance: 3

permissions:
  - tool:invoke[code:read_file, code:review_diff]
  - memory:search[self]

timeout: 600s
parent: housewife
```

---

## Appendix B — Glossary

- **Actor** — any entity that participates in rooms and is subject to clearance
  and permissions. Either a user or an agent.
- **Agent** — an actor defined by a configuration file. Has a role, model tiers,
  feature flags, clearance, and permissions.
- **User** — an actor that authenticates with credentials. Holds implicit root
  authority.
- **Room** — a conversation container with participants, a transcript file, a
  clearance ceiling, and a protocol.
- **Protocol** — a behavioral contract governing how and when agents are invoked
  via RFCs. Enforced by the dispatcher.
- **RFC (Request for Comment)** — the universal invocation primitive. A
  protocol-issued request for an agent to produce a response.
- **System RFC** — an RFC issued by the proactive system or other system
  components, not scoped to a room.
- **Interjection** — a message sent by a non-targeted actor (typically the user)
  while a protocol is awaiting an RFC response. Queued and included in the next
  RFC.
- **Project** — a structural grouping over rooms. Navigation and organization,
  not a permission boundary.
- **Clearance** — data sensitivity tier. Governs what an actor can see. Totally
  ordered integers; higher = more trust.
- **Permission** — discrete action capability. Governs what an actor can do.
- **Permission effect** — `allow`, `require_confirmation`, or `deny`.
- **Config proposal** — a staged config change awaiting user approval.
- **MEMORY.md** — an agent's curated long-term memory file. Always injected
  into context.
- **Daily notes** — per-day markdown files capturing working context and
  observations. Indexed for search.
- **Dreaming** — background consolidation pass promoting durable content from
  daily notes into MEMORY.md.
- **Persistent agent** — long-lived, defined by files on disk.
- **Ephemeral agent** — task-scoped, in-memory only, definition snapshotted to
  audit.
- **Turn limit** — maximum agent-to-agent exchanges without user participation.
- **MCP** — Model Context Protocol. Universal tool integration surface.
- **Explicit cross-clearance handoff** — deliberate declassification of content
  for a lower-clearance recipient. User-confirmed.
- **Compaction** — forcing a dreaming pass on an agent for housekeeping.
