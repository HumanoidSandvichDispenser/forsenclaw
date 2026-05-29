= Agent Lifecycle

== Persistent Agents

Long-lived. Defined by files on disk, exist indefinitely. Stable identity,
accumulated memory, long-term permission grants. The user typically has 1--3.

A persistent agent has:
- A definition file at `$XDG_CONFIG_HOME/forsenClaw/agents/<name>/agent.yaml`.
- A memory directory at `$XDG_DATA_HOME/forsenClaw/agents/<name>/` with
  clearance-stratified `MEMORY-k.md` files and `memory/clearance-k/` daily
  note subdirectories.

The forsen is the default persistent agent --- highest clearance, broadest
permissions, most accumulated context about the user. It is not an
"orchestrator" architecturally --- it's the most trusted agent, and it's good
at orchestration as a consequence of knowing the most. Users are not required
to have an orchestrator; they can talk directly to any agent.

Persistent agents may not self-terminate without user confirmation --- loss of
accumulated memory context is irreversible.

== Ephemeral Agents

Spawned for a task, given scoped context, terminated on completion. Ephemeral
agents exist as in-memory objects with a captured configuration. *Ephemerals
are not a security primitive* --- clearance and OPA handle access scoping.
Ephemerals exist for parallelism and task isolation only.

Characteristics:

- Spawned by a persistent agent or the user.
- Inherit a subset of the spawner's permissions, never more.
- Clearance level equal to or lower than the spawner's.
- Scoped context injection at spawn (not a full memory dump).
- Do not accumulate memory --- no `MEMORY-k.md`, no daily notes, no files on
  disk.
- Discarded after task completion, timeout, room closure, or revocation.
- Definition snapshotted into the audit log at spawn time so references are
  never broken.

Ephemeral agents are where bulk work happens. Most tasks need a focused
context, a defined goal, and teardown --- not a long-lived identity.

== Spawn

The spawner must hold `agent:spawn[<role>]`. The spawner supplies:

- Task description.
- Context injection (dispatcher validates against child's clearance).
- Permission set (subset of spawner's).
- Timeout or termination condition.
- Whether the agent joins an existing room or gets a new one.

The ephemeral agent's definition is snapshotted into the audit log at spawn
time.

== Teardown

Triggers: task completion, timeout, room closure, revocation by parent or user.

On teardown:

- Room transcript already has the full conversation (SQLite messages table).
- Definition snapshot already in audit log (from spawn).
- In-memory agent state is discarded.
- Audit entry records termination cause.

== Compaction

A persistent agent (typically forsen) may trigger compaction of another
persistent agent, subject to `agent:compact[<target>]`. Compaction is a
housekeeping pass that summarizes old room transcript messages into the agent's
daily note at the appropriate clearance level, then advances the per-agent,
per-room `compacted_number` cursor in SQLite.

Compaction is triggered when either of these happens:

- The assembled context exceeds `context.compaction_trigger`.
- The agent session ends after 30 minutes of inactivity.

=== Batch Sizing

The batch size is dynamic. On a byte-threshold trigger, forsenClaw walks
forward from `compacted_number`, accumulating message sizes until the removed
bytes are enough to bring the assembled context down toward
`context.compaction_target`. That batch is capped so compaction never crosses
the guaranteed tail window (`context.minimum_guaranteed`). If the remaining
uncompacted tail is smaller than `min_compaction_batch`, compaction is skipped.

Session-end compaction compacts all messages outside the guaranteed window,
regardless of byte size. If even compacting everything outside the guaranteed
window still leaves the context above target, forsenClaw proceeds anyway. The
floor is hard; the model handles the larger context.

=== Compaction Request Flow

Before a real Request is assembled, the dispatcher may issue a system
compaction Request that tells the agent to summarize a specific room/message
range into its daily note. When compaction succeeds, the cursor advances to the
end of the compacted batch and the real Request is assembled afterward. If
compaction fails (model error, timeout), the real Request still proceeds with
the uncompacted context.

== SLM Harness Considerations

The invocation pipeline is the correct layer for small-model scaffolding.
Techniques for SLMs (per-turn skill injection, output repair for malformed tool
calls, quality monitoring for empty/looping responses, thinking-budget caps)
apply at the Request processing level, transparent to the agent definition and
protocol.
