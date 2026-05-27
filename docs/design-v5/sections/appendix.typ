= Appendix

== Agent Configuration Examples

=== The Housewife --- Most Trusted Persistent Agent

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
  - config:read[self]
  - config:write[self]
  - config:read[server]
  - config:write[server]
  - proactive:enable
  - proactive:act[low, medium]
  - handoff:propose[*]
  - policy:propose
  - permission:grant  # condition: granted <= self
```

=== Scout --- Lower-Clearance External Agent

```yaml
name: scout
role_description: >
  Handles external-facing tasks: web searches, API calls, public data
  retrieval. Lower clearance; assembles only external-safe context. Good
  for tasks where data leaves the system.

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

=== Ephemeral Task Agent

In-memory only, definition snapshotted to audit at spawn time.

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

== Network Topology

```
┌──────────────────────────────────────────────────────┐
│ NAS / Host                                           │
│   • forsenClaw server (single Go binary)             │
│   • Agent files: definitions + memory (XDG paths)    │
│   • MCP servers: Gmail, Drive, Calendar, etc.        │
│   • Orchestration, scheduler, dispatcher             │
│   • OPA policy engine (embedded)                     │
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

Every connection is token-authenticated regardless of network. LAN is
convenient, not trusted. Tokens are scoped per client and revocable from the
server.

== Model Provider Registry

All model calls go through a provider registry defined in `hearth.yaml`.

```yaml
providers:
  - name: anthropic
    protocol: anthropic
    base_url: https://api.anthropic.com
    api_key: "${ANTHROPIC_API_KEY}"

  - name: ollama
    protocol: openai_compatible
    base_url: http://ollama-docker-container-name:11434
    # tool_mode: xml  # use "xml" for local models with no native tool support

models:
  claude-sonnet-4.6:
    provider: anthropic
    provider_model: claude-sonnet-4-20250514

  gemma-4-local:
    provider: ollama
    provider_model: gemma3:12b

tools:
  max_tool_iterations: 10
  webfetch:
    clearance: 1
  brave_search:
    api_key: "${BRAVE_API_KEY}"
    clearance: 1
  servers:
    - name: email
      url: "https://..."
      clearance: 2
    - name: calendar
      url: "https://..."
      clearance: 3
    - name: finances
      url: "https://..."
      clearance: 5
```

- Agents reference model strings (`claude-sonnet-4.6`, `gemma-4-local`).
- The registry resolves to provider + provider-specific model ID.
- Inference uses a single `Infer(...)` entrypoint dispatching to a protocol
  adapter.
- Streaming is first-class: adapters yield normalized chunks.
- API keys resolved from environment variables, never stored in config files.

== v1 Scope and Phasing

=== v1 --- Usable System

The minimum viable forsenClaw: a single-user system where you can chat with
persistent agents who remember things, use MCP tools, and manage rooms.

*Must have:*

- Actor model (user login + file-based agent definitions).
- Persistent and ephemeral agents with feature flags.
- Clearance-stratified memory: MEMORY-k.md + clearance-k daily notes.
- Rooms with FreeForm protocol (2 participants).
- Request-based invocation, dispatcher, context assembly.
- Cross-room feed plus bounded current-room tail in context assembly.
- Tail reads from transcript end, keyed by per-agent, per-room compaction
  cursor.
- Compaction Request flow and `compacted_number` persistence.
- BLP enforcement (no read-up, no write-down) at assembly, send, spawn.
- OPA-backed ABAC for permissions; default policy file shipped with binary.
- MCP tool integration with static schema injection.
- Model provider registry (Anthropic + Ollama) in `hearth.yaml`.
- Request DAG: node/edge tracking, cycle detection, audit integration.
- Room metadata, messages, and audit in SQLite.
- Web frontend: room list, room view, DMs, agent config viewer, settings,
  clearance ceiling filter, room clearance shift control.
- Audit log with DAG traversal view.
- Single Go binary deployment.
- XDG-compliant storage layout.

*Nice to have in v1:*

- RoundRobin and FireAndForget protocols.
- Broadcast protocol.
- Projects as structural grouping.
- Proactive event Requests via pub/sub.
- Basic notification delivery (in-room + badge counts).
- Config and policy diff review UI.
- Dreaming pass.

*Deferred to v2:*

- IterativeDraft protocol.
- Custom protocol plugin interface.
- Dynamic MCP tool injection (relevance filtering).
- Client daemon for local tools.
- Knowledge base MCP server.
- Push notifications.
- Cost accounting per agent/room.
- Memory budget management (auto-truncation of MEMORY-k.md).
- RAG-enabled context injection.
- Export/import room + agent bundles.

=== Build Order

+ *Storage layout + server config.* Establish XDG paths, `hearth.yaml`
  parsing, clearance level config, agent definition loading from disk.
+ *Model provider registry + inference adapter.* Get Ollama and Anthropic
  calls working. Everything else depends on this.
+ *Agent definitions + clearance-stratified memory.* MEMORY-k.md, daily notes
  per clearance level, search index. Test with a single agent reading and
  writing its own memory.
+ *Rooms + FreeForm protocol + Request dispatcher.* The messaging backbone.
  Room metadata in SQLite, messages and compaction cursors in SQLite. User can chat with an agent.
+ *BLP enforcement + OPA permission evaluation.* Enforce at the three boundary
  points. Add audit logging. Implement config proposal and approval flow.
+ *MCP integration.* Register MCP servers, inject tool schemas, route calls.
  Test with a real MCP server (e.g., filesystem).
+ *Request DAG.* Node/edge tracking, cycle detection, blocked-node resolution,
  audit DAG trace.
+ *Session lifecycle + dreaming.* Session end triggers, dreaming pass from
  daily notes → MEMORY-k.md at appropriate level.
+ *Frontend.* Room list, room view, agent config, settings, clearance ceiling
  filter, room clearance shift, config diff review. Connect via WebSocket.
+ *Additional protocols.* RoundRobin, Broadcast, FireAndForget. Test
  structured debate.
+ *Projects.* Grouping, summary view, child room management.
+ *Proactive system.* Event Requests via pub/sub, scheduled triggers, risk
  tiers, confidence gating.

== Open Questions

=== Search Index Location

Should the search index live in `$XDG_CACHE_HOME` (rebuildable, excluded from
backups) or `$XDG_DATA_HOME/db/` (persistent)? Rebuilding the index requires
re-embedding all memory files, which costs time and compute. If the embedding
model changes, the index must be rebuilt anyway. Initial approach: cache, with
a rebuild command (`forsenClaw index --rebuild`).

=== Dreaming Quality

Dreaming effectiveness depends on the routine model's ability to judge what is
durable vs. transient, and what clearance level a promoted fact belongs to.
What prompting strategy yields useful promotions at the right level? Initial
approach: structured prompt asking for facts, preferences, and decisions that
would be useful "next month," with an explicit step to classify each fact's
minimum required clearance. Refined empirically.

=== Retrieval Relevance

Which embedding model? What chunking for daily notes across clearance levels?
Re-ranking? Initial approach: nomic-embed-text via Ollama, per-paragraph
chunking, hybrid keyword + vector search, filtered by clearance at query time.

=== MEMORY-k.md Budget

How large before truncation? What's the right budget relative to context window
size? Initial approach: configurable per-agent per-level, default 4K tokens per
level. Truncation keeps the top of the file (most curated content tends to be
organized top-down).

=== Protocol State Persistence

RoundRobin and Broadcast have state (whose turn, round count, collected
responses). This lives in `rooms.db` and is reconstructed on server restart.

=== FreeForm Agent-Agent Termination

For FreeForm rooms with two agents: beyond the turn limit, how do agents signal
"we're done"? Initial approach: don't try to detect convergence. Use `max_turns`
as the hard stop. Agents can explicitly signal completion by including a
structured marker in their response.

=== Transcript Growth

Resolved by compaction and cursor-based tail reads. SQLite message table still
grow on disk, but older messages are compacted into daily notes and excluded from
later assemblies via the per-agent, per-room `compacted_number` cursor.

=== Cost Accounting

Cloud model calls cost money. Track per-agent or per-room? Surface budgets?
Initial approach: log token usage per invocation in audit; surface aggregates
in UI; budget enforcement is v2.

=== OPA Policy Bootstrapping

What is the default policy shipped with the binary? It should be conservative
(deny-first, require_confirmation for most agent actions) while allowing the
housewife to function without extensive manual configuration. Policy is shipped
as a file so the user can inspect and customize it immediately.

== Glossary

/ Actor: Any entity that participates in rooms and is subject to clearance and
  permissions. Either a user or an agent.
/ Agent: An actor defined by a configuration file. Has a role, model tiers,
  feature flags, clearance, and permissions.
/ User: An actor that authenticates with credentials. Holds implicit root
  authority.
/ Room: A conversation container with participants, a transcript file, a
  clearance ceiling, and a protocol.
/ Protocol: A behavioral contract governing how and when agents are invoked via
  Requests. Enforced by the dispatcher.
/ Request: The universal invocation primitive. A dispatcher-issued instruction
  for an agent to produce a response. Sources: room | system | event.
/ Event Request: A Request issued by the pub/sub system in response to an
  external trigger (email, calendar, webhook, schedule).
/ Interjection: A message sent by a non-targeted actor (typically the user)
  while a protocol is awaiting a Request response. Queued and included in the
  next Request.
/ Project: A structural grouping over rooms. Navigation and organization, not a
  permission boundary.
/ Clearance: Data sensitivity level. Governs what an actor can see and what
  memory strata are assembled into context. Totally ordered integers; higher =
  more trust.
/ Context scope: The set of memory strata available during a room interaction,
  determined by the room's clearance ceiling.
/ BLP (Bell-LaPadula): Data flow policy enforcing no read-up and no write-down
  without explicit approval.
/ ABAC (Attribute-Based Access Control): Action policy model implemented via
  OPA and Rego. Governs what an actor can do.
/ OPA (Open Policy Agent): Embedded policy engine. Evaluates permission
  decisions against a Rego policy file.
/ Permission: Discrete action capability evaluated by OPA. Governs what an
  actor can do.
/ Permission effect: `allow`, `require_confirmation`, or `deny`.
/ Config proposal: A staged config or policy change awaiting user approval.
/ MEMORY-k.md: An agent's curated long-term memory file at clearance level k.
  Context assembled at clearance level n includes all MEMORY-k.md where k ≤ n.
/ Daily notes: Per-day Markdown files at `memory/clearance-k/YYYY-MM-DD.md`.
  Indexed for search.
/ Dreaming: Background consolidation pass promoting durable content from daily
  notes into the appropriate MEMORY-k.md.
/ Persistent agent: Long-lived, defined by files on disk.
/ Ephemeral agent: Task-scoped, in-memory only. Exists for parallelism and
  task isolation; not a security primitive.
/ Turn limit: Maximum agent-to-agent exchanges without user participation.
/ MCP: Model Context Protocol. Universal tool integration surface.
/ Explicit cross-clearance handoff: Deliberate declassification of content for
  a lower-clearance recipient. User-confirmed.
/ Compaction: Summarizing old room transcript messages into daily notes and
  advancing the `compacted_number` cursor.
/ Request DAG: Dependency graph of in-flight Requests. Edges model blocking
  relationships; enables concurrent non-blocked Request processing.
/ DLP (Data Loss Prevention): Structural guarantee that agents cannot exfiltrate
  data above the current room clearance, because higher-clearance data is absent
  from the assembled context.
