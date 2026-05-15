Actionable implementation checklist for Hearth v1 — backend first.

Purpose: a developer-focused, actionable checklist split by feature. Backend tasks are primary; frontend work is optional and listed as follow-ups after each backend feature.

Notes:
- Work items are grouped to match the DESIGN-v4 build order and feature areas.
- Prefer small, testable increments. Implementations should include basic unit/integration tests where practical.

F1. Storage Layout + Server Config
- [ ] Implement XDG path resolution for config/data/cache
- [ ] Define Go structs for `hearth.yaml` and parse YAML on startup
- [ ] Define Go structs for `agent.yaml` and validate on load
- [ ] Bootstrap directories on first run: config agents/, data agents/, rooms/, staging/, db/, cache/
- [ ] Add CLI/env override for config path
- [ ] Implement config validation (required fields, model references, clearance ranges)

Frontend follow-up (optional):
- [ ] Settings page to view/edit `hearth.yaml` (read-only by default)

F2. Model Provider Registry + Inference
- [ ] Define Provider interface with an `Infer(...)` entrypoint supporting streaming chunks
- [ ] Implement adapter for Ollama (OpenAI-compatible) with streaming
- [ ] Implement adapter for Anthropic (streaming)
- [ ] Implement provider registry: resolve model string → provider + provider_model
- [ ] Load API keys from env vars only (never write to files)
- [ ] Add model-tier mapping (primary/routine/sensitive) resolution
- [ ] Unit tests for registry and adapter normalization

Frontend follow-up (optional):
- [ ] Provider & model admin UI (list providers, test calls)

F3. Agent Definitions + Memory Files
- [ ] Load agent definitions from `$XDG_CONFIG_HOME/hearth/agents/*/agent.yaml` on startup
- [ ] Hot-reload agent definitions on file changes (fsnotify/poll)
- [ ] Implement read/write for `MEMORY.md` per agent
- [ ] Implement daily notes at `memory/YYYY-MM-DD.md` (read today + yesterday; write observations)
- [ ] Implement MEMORY.md truncation policy (configurable per-agent; default ≈ 4K tokens)
- [ ] Implement context assembly pipeline (system prompt → MEMORY.md → daily notes → RAG → MCP tools → room history → turn budget → RFC payload)
- [ ] Implement search index (SQLite hybrid) and embedding pipeline (Ollama embeddings)
- [ ] Implement `hearth index --rebuild` command to rebuild index from files
- [ ] RAG retrieval honoring requesting agent's clearance

Frontend follow-up (optional):
- [ ] Agent memory viewer: MEMORY.md, daily notes, DREAMS.md

F4. Rooms + FreeForm Protocol + RFC Dispatcher
- [ ] Define Room, Message, RFC, RFCPayload, Actor types
- [ ] SQLite schema: rooms table (participants, clearance_ceiling, protocol_type, protocol_state)
- [ ] Implement JSONL append-only transcript writer for rooms
- [ ] Implement Dispatcher interface and per-agent RFC queue
- [ ] Implement Protocol interface and the FreeForm protocol (2 participants)
- [ ] Implement per-agent goroutine lifecycle: wake on RFC, process queue, sleep when empty
- [ ] Implement interjection behavior (queue user messages while agent mid-response)
- [ ] REST endpoints: create room, post message, get room/messages
- [ ] WebSocket / real-time hub for room updates
- [ ] Enforce turn limits (soft/hard) with extension request flow

Frontend follow-up (optional):
- [ ] Room list, room view, message composer, real-time updates (WS)

F5. Clearance + Permissions + Config Staging
- [ ] Implement clearance tiers (configurable integers) and default 5-tier scheme
- [ ] Enforce clearance at: retrieval (search), outgoing message send (room ceiling), spawn-time context injection
- [ ] Implement message classification and capping to room ceiling
- [ ] Implement permission model (action + scope + effect) and evaluation order (default deny, explicit deny wins, most-specific scope wins, require_confirmation > allow on tie)
- [ ] Implement permission categories: tool:invoke, room:*, memory:*, agent:*, config:*, proactive:*, handoff:propose, audit:read
- [ ] Implement config staging flow: write proposed file to staging, compute diff for `require_confirmation`, allow/approve/reject handling
- [ ] Single pending proposal per agent per file; snapshot audit entries for proposals and outcomes

Frontend follow-up (optional):
- [ ] Config proposal review UI (diff view, approve/reject)

F6. MCP Integration
- [ ] Implement MCP client basics and tool registry (register servers + tool schemas)
- [ ] Static tool injection at RFC time based on `tool:invoke[...]` permissions
- [ ] Dispatcher executes permitted tool calls on behalf of agents; validate results and log audit
- [ ] Extract and parse tool calls from model responses; feed results back into agent flow
- [ ] Handle unreachable tool hosts with clear failure modes (no silent fallback)

Frontend follow-up (optional):
- [ ] Tools page: list available MCP servers, tools, and test invocations

F7. Session Lifecycle + Dreaming
- [ ] Implement per-agent session tracking and 30-minute inactivity timeout
- [ ] Ensure in-session writes go to today's daily note
- [ ] On session end, if `dreaming=on`, schedule non-blocking consolidation pass
- [ ] Implement dreaming pass (use routine_model, structured prompt → promote durable content → append to MEMORY.md and write DREAMS.md summary)
- [ ] Ensure proactive RFCs run with isolated context (no room history)

Frontend follow-up (optional):
- [ ] DREAMS log viewer and dreaming activity audit

F8. Audit Logging
- [ ] Implement append-only audit SQLite schema and writer
- [ ] Record: tool invocations, permission denials, require_confirmation checks, config proposals (diff + outcome), agent spawn/teardown snapshots, cross-clearance rejections, handoff proposals/resolutions
- [ ] Expose audit read API for users; gate agent access via `audit:read` permission

F9. Ephemeral Agents (Spawn/Teardown)
- [ ] Implement spawn API flow: parent supplies task, context injection, permission subset, timeout, room join behavior
- [ ] Enforce `agent:spawn[<role>]` permission
- [ ] Snapshot ephemeral agent definition to audit at spawn
- [ ] Terminate ephemeral agent on completion/timeout/teardown; record audit entry

F10. Agent Compaction
- [ ] Implement `agent:compact[<target>]` permission check and API
- [ ] Trigger dreaming pass for compaction and audit the event

F11. User Authentication
- [ ] Implement user model with secure password hashing (bcrypt) and session tokens
- [ ] Implement login/logout endpoints and token revocation
- [ ] Auth middleware for all API routes (user = implicit root)

F12. API Layer + Server Infrastructure
- [ ] Wire HTTP router + middleware (logging, auth, error handling)
- [ ] Implement REST endpoints for rooms, messages, agents, proposals, audit, search, memory
- [ ] Implement graceful shutdown (drain RFCs, close DB, flush files)
- [ ] Build single Go binary; plan to embed frontend assets later

F13. Additional Protocols (v1 nice-to-have)
- [ ] RoundRobin: sequential RFC issuance, turn_order, max_rounds, include_user
- [ ] FireAndForget: single RFC then close, timeout/result_to
- [ ] Broadcast: simultaneous RFCs, collect responses, optional reconciliation

F14. Proactive System (v1 nice-to-have)
- [ ] Scheduled tick system (cron-like schedules per agent) and interrupt triggers
- [ ] System RFC issuance path distinct from room RFCs (isolated context)
- [ ] Risk tiers and dispatcher enforcement (low/medium/high → act/approval/user confirm)
- [ ] Confidence gating: surface suggestions below threshold
- [ ] Deliver proactive outputs to rooms or system notification room

F15. Projects (v1 nice-to-have)
- [ ] Project model (name, description, child rooms, associated agents, clearance ceiling)
- [ ] CRUD and summary endpoints; project-level clearance inheritance

Open questions / low-priority items to capture for v2 (do not block v1):
- [ ] Dynamic MCP tool injection (relevance filtering)
- [ ] IterativeDraft protocol and custom protocol plugin interface
- [ ] Cross-clearance handoff UI (side-by-side diff review)
- [ ] Cost accounting and budget enforcement per agent/room
- [ ] Memory auto-truncation policy refinements and export/import bundles

How to use this file
- Pick one feature area (F1..F4 recommended to start). Create small PRs that implement a single checklist group at a time.
- Add tests and basic integration checks for each completed item.
- For user-visible changes, add minimal frontend stubs only after the matching backend API is stable.

End
