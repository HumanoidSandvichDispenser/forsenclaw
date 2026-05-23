= Overview

forsenClaw is a self-hosted multi-agent orchestration system with file-based
agent definitions, clearance-stratified memory, OPA-based access control, and
pub/sub-driven rooms. It runs on a NAS or similar always-on host.

Users and agents are both *actors* in the system. The only structural
difference: users authenticate with credentials; agents are defined by
configuration files on disk and invoked by the system via Requests. Both
participate in rooms, send messages, hold permissions, and are subject to
clearance.

The design supports both *companion-style* agents (long-lived identity,
accumulated memory, proactive behavior) and *task-style* agents (ephemeral,
scoped context, reactive). These are configurations of the same agent
primitive, controlled by per-agent feature flags.

== Goals

- Persistent agent identities that accumulate memory across sessions.
- Clean separation between agent identity and the underlying model.
- Clearance as a context scope, not merely an access filter. The room's
  clearance level determines which memory strata the agent can see, forming a
  structural DLP boundary.
- Two orthogonal access axes --- clearance (data) and permissions (actions) ---
  enforced at distinct boundaries via BLP and OPA-backed ABAC.
- Multi-tier model routing per agent (primary / routine / sensitive).
- MCP as the universal tool integration surface.
- Protocol-driven rooms where protocols actively orchestrate agent invocation
  rather than passively gating sends.
- File-based agent definitions and memory, XDG-compliant.
- Self-hosted, single-user, sovereign over its own data. No required cloud
  dependency.
- Single binary. Target idle footprint: ~50--80 MB.

== Non-Goals

- Not a multi-tenant SaaS or cloud product.
- Not a replacement for coding harnesses; forsenClaw integrates them as MCP
  tool surfaces.
- Not a single-model chatbot wrapper.
- Not a competitor to general-purpose task orchestration frameworks (LangChain,
  AutoGen). forsenClaw's distinguishing feature is the unified companion + task
  model under a single permission and memory architecture.

== Design Principles

/ Identity lives in memory, not in context: Agents feel continuous because
  their memory files are continuous; the active context window is bounded and
  assembled per-invocation.

/ Clearance is a context scope, not a filter: The room clearance determines
  which memory strata are present in the assembled context. An agent in a
  clearance-2 room does not have clearance-4 data available --- it cannot leak
  what it cannot see. This is a structural DLP guarantee, not just an
  enforcement layer.

/ Defense in depth at the boundary, not the storage: Memory is stored at full
  fidelity per stratum; clearance is enforced at assembly, retrieval, send, and
  context injection --- not by deleting or summarizing data on disk.

/ Stateless models, stateful agents: Models are compute primitives. Agents are
  the persistent identity that owns context, memory access, and permissions.

/ No ambient authority: Every privileged action --- tool dispatch, room
  creation, memory write, config change --- is gated by an explicit permission
  grant evaluated by OPA.

/ Trust is at the agent, not the model: An agent's clearance determines what
  data it may see. Within that clearance, the agent may route calls to any of
  its configured models.

/ Protocols orchestrate; agents respond: Protocols actively issue Requests to
  agents. Agents do not decide when to speak --- the protocol does. New
  interaction patterns are new protocols, not new primitives.

/ Files over databases for identity and knowledge: Agent definitions and memory
  are files on disk --- readable, editable, version-controllable without a
  running server. Databases are for queryable runtime state and audit.

/ Requests as the universal invocation primitive: Room protocols, the proactive
  system, and event pub/sub all deliver work to agents through the same Request
  queue. There is no separate invocation path.

== Tech Stack

/ Backend: Go
  - Concurrency: goroutines + channels.
  - Database driver: standard `database/sql` with modernc.org/sqlite (pure-Go
    SQLite, no CGO).
  - Migrations: goose or golang-migrate.
  - Policy evaluation: embedded OPA (Open Policy Agent) with Rego.
  - Scheduling: time.Ticker + priority queue for proactive ticks.

/ Frontend: Vue 3 + TypeScript + Pinia

/ Storage:
  - Agent definitions: YAML files on disk.
  - Agent memory: clearance-stratified Markdown files on disk
    (`MEMORY-k.md`, `memory/clearance-k/YYYY-MM-DD.md`).
  - Room transcripts: JSONL files on disk.
  - Room metadata, protocol state, request DAG, audit log: SQLite.
  - Search index: SQLite (cache-tier, rebuildable).

/ Local inference: Ollama over HTTP (not in-process).
