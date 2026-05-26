= MCP Integration

MCP (Model Context Protocol) is the universal tool integration surface. All
external capabilities --- email, calendar, file access, code execution, web
search, system management, knowledge bases --- are exposed as MCP tool servers.

== Architecture

MCP servers run as separate processes (or remote services) and register with
forsenClaw's tool registry. Each server exposes a set of tools with schemas.
forsenClaw acts as the MCP client --- the dispatcher invokes tools on behalf of
agents.

== Tool Injection

*v1: static injection.* At invocation time, MCP tool schemas the agent is
permitted to use (per its `tool:invoke[...]` permissions, evaluated by OPA)
and cleared to see (per BLP tool clearance, evaluated against the room ceiling)
are resolved and injected into the API call. Tools whose clearance exceeds the
room ceiling are not injected --- they are structurally absent, same as
higher-clearance memory files. Tools below the room ceiling are injected with a
clearance annotation indicating the write-down risk.

*v2 candidate: dynamic injection.* For agents with broad permissions and many
available tools, a relevance filter matches the Request payload against tool
descriptions and injects only top-K relevant schemas. Reduces context bloat
without changing the agent or permission model.

== Tool Routing

Tools may be hosted in two locations:

+ *On the server* --- built-in MCP servers (web search, system tools). Always
  reachable.
+ *Remote services* --- third-party MCP servers accessed over the network.

If a tool's host is unreachable, the call fails --- no fallback.

== Knowledge Base

The knowledge base is an MCP tool surface, not a custom forsenClaw primitive.
Options:

- An existing knowledge-base solution (Obsidian + MCP adapter, dedicated RAG
  service) exposed as MCP tools.
- A simple forsenClaw-hosted document store with MCP tools --- if no external
  solution fits.

Agents interact with the knowledge base through normal tool invocation, subject
to OPA permission evaluation.

== Permission Integration

MCP tool invocation is gated by `tool:invoke[<tool_id>]` permissions evaluated
by OPA. The OPA policy receives the full invocation context --- agent identity,
room clearance, tool ID, arguments, time-of-day, and any other relevant
attributes --- and returns `allow`, `require_confirmation`, or `deny`.

An agent permitted `tool:invoke[email:*]` with `require_confirmation` for
`tool:invoke[email:send]` can read email freely but needs user approval to
send. This confirmation creates a blocked node in the Request DAG (see @rooms)
that resolves on user response.

OPA evaluation runs in the dispatch layer *before* the executor is invoked. The
executor itself is a pure transport layer: it resolves the tool, converts
arguments, calls the MCP client, and records the audit event. It assumes OPA
has already approved the call. This separation keeps the executor stateless and
testable independently of the policy engine.

== DLP Boundary

The MCP tool integration surface is bounded by the current room clearance at
two points:

*Context-side DLP.* Tools can only exfiltrate data present in the assembled
context. Since the context is assembled at the room's clearance level,
higher-clearance data is structurally absent --- it cannot be leaked even if a
tool call attempts to transmit it.

*Tool-side DLP.* Tools themselves carry clearance levels (see
@access-control). A clearance-5 finance tool called from a clearance-2 room
cannot be invoked: it is not injected (no read-up). A clearance-2 email tool
called from a clearance-4 room requires confirmation: any outbound data is a
potential write-down. Tool clearance is a data classification orthogonal to
`tool:invoke[...]` permissions.
