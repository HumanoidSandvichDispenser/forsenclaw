= Memory Architecture <memory>

forsenClaw's memory is *files on disk* that agents read and write. The model
only "remembers" what is saved to files --- there is no hidden state.

Memory is *owned by agents, not rooms*. Each persistent agent has its own
memory directory under `$XDG_DATA_HOME/forsenClaw/agents/<name>/`. Memory is
organized in clearance strata: each file contains only information relevant to
its level, and context assembly combines strata additively.

== Clearance-Stratified Memory Files

Instead of a single `MEMORY.md`, each agent maintains a stack of files, one
per clearance level that has content:

```
agents/housewife/
  MEMORY-1.md          ← public-facing role, communication style
  MEMORY-2.md          ← external-safe preferences, timezone
  MEMORY-3.md          ← active projects, professional context
  MEMORY-4.md          ← full personal context (dreaming target)
  MEMORY-5.md          ← vault: health, finances (manual only)
  memory/
    clearance-2/
      2026-05-23.md
    clearance-4/
      2026-05-23.md
    clearance-5/
      2026-05-23.md
```

*Files are additive, not duplicated.* `MEMORY-3.md` contains only what is new
at level 3, not a copy of levels 1 and 2. When assembling context at clearance
3, the assembler reads `MEMORY-1.md`, `MEMORY-2.md`, and `MEMORY-3.md` in
order and concatenates them. The agent sees a coherent, unified memory at its
current level.

Files are only created when there is content to write at that level. An agent
that has never had a clearance-5 interaction will not have `MEMORY-5.md`.

All files are plain Markdown --- human-editable, diffable, and git-trackable.

=== Memory Flow Rules

/ Writes happen at current clearance: A memory write during a room interaction
  targets the stratum matching the room's current clearance level.

/ Data flows down via explicit redaction approval: An agent wishing to pass
  sensitive content to a lower-clearance stratum generates a redaction proposal
  using its `sensitive_model`. The user reviews the original and proposed
  redaction side-by-side, optionally edits, and approves or rejects. Only on
  approval does the content cross the boundary.

/ Data flows up via dreaming or manual promotion: The dreaming pass
  consolidates daily notes and may promote facts to a higher level (see
  Dreaming). Manual promotion: the user inspects a diff and instructs the agent
  to promote a fact. The agent writes to the higher stratum after the user
  approves.

/ Destructive operations require user confirmation: Forgetting facts (deleting
  or overwriting established memory) requires user confirmation regardless of
  who issues the instruction. This prevents accidental or adversarial memory
  wipes.

=== Clearance-Level Granularity

Lower-clearance representations of a fact are not conflicts with
higher-clearance representations --- they are the same fact at different
granularity. For example:

- `MEMORY-2.md`: "User works in tech."
- `MEMORY-4.md`: "User is a CS student at UNR working on a self-hosted
  multi-agent system called forsenClaw."

These coexist. The dreaming pass understands this and does not treat
granularity differences as conflicts.

== Daily Notes --- Working Memory

Per-day Markdown files at `memory/clearance-k/YYYY-MM-DD.md`. Running context,
observations, session summaries, detailed notes. Today's and yesterday's notes
for all levels ≤ room clearance are loaded automatically into context.

Daily notes are the working layer. Agents write to them during sessions,
capturing observations, decisions, and reasoning that may be useful later but
is not yet curated into a `MEMORY-k.md` file.

Compaction summaries from older room transcripts are appended here so they fall
outside the current-room window on later assemblies without mutating the
transcript itself.

== Dreaming --- Background Consolidation

For agents with `dreaming=on`, a background pass periodically reviews daily
notes and promotes durable material into the appropriate `MEMORY-k.md` file.
This runs on the `routine_model` to keep costs low.

*Dreaming runs at the agent's full clearance.* It sees all strata and all daily
notes. Conflict resolution:

- Higher-clearance fact + recency wins.
- Granularity differences (same fact at different levels) are not conflicts.
- Genuinely ambiguous conflicts (same level, different claims) are surfaced
  for user review rather than silently resolved.

Dreaming output defaults to level 4 (personal). Level 5 (vault) facts are
never promoted automatically --- they are always manually curated.

The dreaming pass writes a brief summary of what it promoted and why to a
`DREAMS.md` file for human review.

== Search Index

A SQLite-backed hybrid search index (vector similarity + keyword matching) over
all `MEMORY-k.md` files and daily notes. Embeddings computed locally via
Ollama.

The search index is infrastructure, not a memory layer. It is rebuildable from
files on disk at any time and lives in `$XDG_CACHE_HOME/forsenClaw/`.

Retrieval is parameterized by the requesting agent's clearance level. An agent
searches only memory strata at or below its current clearance.

With `rag: false` (the v1 default), no embedding model is loaded and no index
worker starts.

== Sessions and Lifecycle

A *session* is a bounded period of agent activity.

- Ends after 30 minutes of inactivity on a per-agent basis.
- Within a session: agents write observations to today's daily note at the
  appropriate clearance level.
- At session end: if there are uncompacted messages outside the guaranteed
  window, a compaction pass is scheduled. If `dreaming=on`, a consolidation
  pass may also run.
- Event Requests run in isolated session contexts --- they do not extend or
  contaminate user-facing sessions.

== Room Transcripts --- Shared Conversation History

Room transcripts are stored as JSONL files at
`$XDG_DATA_HOME/forsenClaw/rooms/<room-id>.jsonl`. Each line is a message
record:

```json
{
  "id": "msg_001",
  "timestamp": "2026-05-14T10:30:00Z",
  "room": "room_abc",
  "sender": "housewife",
  "clearance_tag": 4,
  "type": "message",
  "content": "..."
}
```

Transcripts are append-only and represent the shared record of what happened in
a room, distinct from agent memory (an agent's personal understanding of what
matters).

The active tail of each transcript is windowed by a per-agent, per-room
compaction cursor stored in SQLite. Context assembly never re-injects messages
before that cursor.
