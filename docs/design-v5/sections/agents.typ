= Agent Model <agents>

An *agent* is a persistent identity defined by:

+ A *definition file* (`agent.yaml`) --- role, system prompt, behavioral
  configuration, model assignments, feature flags, clearance, permissions.
+ A *memory directory* --- clearance-stratified `MEMORY-k.md` files for
  long-term memory, and `memory/clearance-k/YYYY-MM-DD.md` for daily notes.

The agent owns its context window assembly, its memory files, its permission
set, and its identity across sessions. Models are stateless compute that the
agent invokes.

== Multi-Tier Model Assignment

Each agent declares three model tiers. Tiers describe *routing roles*, not
trust boundaries --- an agent's clearance governs what data it sees regardless
of which tier handles the call.

/ `primary_model`: High-reasoning tasks. Examples: Claude Sonnet, GPT-class.
/ `routine_model`: Low-stakes ticks, proactive checks, summarization. Examples:
  local Gemma, local Llama.
/ `sensitive_model`: The model for operations the user prefers to keep local or
  routed to a trusted provider. Typically local. Used by default for redaction
  proposals and other sensitivity-aware operations.

The agent decides which tier to invoke based on the task. This is not model
fallback --- tiers have different roles, not different quality levels.

== Feature Flags

#table(
  columns: (auto, 1fr, 1fr),
  table.header([*Flag*], [*On*], [*Off*]),
  [`identity_continuity`],
  [MEMORY-k.md files persist across sessions],
  [Memory starts fresh each session],

  [`daily_notes`],
  [Daily note files maintained, indexed for search],
  [No daily notes; context from MEMORY-k.md only],

  [`proactive_triggers`],
  [Participates in event-driven and scheduled Requests],
  [Acts only when invoked via room protocol Request],

  [`dreaming`],
  [Background consolidation from daily notes to MEMORY-k.md],
  [No automatic promotion; manual curation only],

  [`rag`],
  [Index worker starts; retrieval chunks may be injected],
  [No embedding model, no index worker; RAGChunks stays nil],
)

Companion-style agents typically have all flags on. Task-style agents typically
have all flags off.

== Context Assembly

Assembled fresh each invocation (per Request). The assembly differs by Request
source.

=== Room Requests (Full Assembly)

Order, narrowing from background to immediate:

+ *System prompt* --- from `agent.yaml` role description, plus a clearance
  notice injected by the assembler (see @access-control).
+ *MEMORY-k.md files* --- all files where k ≤ room clearance, read in
  ascending order and concatenated. Additive: each file contains only what is
  new at that level.
+ *Daily notes* --- today's and yesterday's notes for all clearance levels k ≤
  room clearance, if `daily_notes=on`.
+ *Cross-room feed* --- recent messages from all other rooms the agent
  participates in, labeled by room ID and merged chronologically. Filtered by
  the agent's clearance.
+ *Current room history* --- windowed tail of the target room's transcript,
  starting at the room's `compacted_number`.
+ *Turn budget notice* --- remaining turns before user approval required.
+ *Request payload* --- the actual message the agent is responding to,
  including any pending interjections.

The assembly narrows from ambient background context down to the immediate
task. Current room history is placed adjacent to the request payload so
recency bias works correctly for the turn being formed.

=== Event Requests (Minimal Assembly)

+ System prompt + clearance notice.
+ MEMORY-k.md files (all k ≤ agent's full clearance).
+ Daily notes (today and yesterday, all clearance levels).
+ Event payload.

No cross-room feed, no room history. Event Requests run in isolated context to
prevent session contamination and keep inactivity timers honest.

=== Cross-Room Feed

At assembly time, query SQLite for all rooms where the agent participates,
excluding the current room. Read the last `other_room_window` messages from
each room, filter by clearance, merge chronologically, and format each message
as `[#room-id --- relative-time] Sender: Content`. If the agent participates
in no other rooms, this tier is empty.

=== Current Room History

Read only the tail of the current room transcript, starting from that room's
`compacted_number`. The store returns messages with `number > compacted_number`
in chronological order, bounded by the configured window. This keeps the hot
path bounded even for large rooms.

== Agents Are Not Knowledge Specialists

LLMs already have broad knowledge. The role of a persistent agent is not
"knows about cooking" or "knows about code" --- it is defined by *what it knows
about the user* (clearance), *what it can do* (permissions), and *what it's
responsible for* (role). The housewife is the most trusted agent not because it
knows the most about the world, but because it knows the most about *you*. A
lower-clearance agent isn't dumber --- it has less context about your life.
