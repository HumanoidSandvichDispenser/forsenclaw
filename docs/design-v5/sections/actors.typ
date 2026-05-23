= Actor Model

An *actor* is any entity that participates in rooms, sends messages, and is
subject to clearance and permissions. There are two kinds.

== Users

A user authenticates with credentials (v1: username + password; later:
hardware token, biometric). On authentication, the user receives a session
token scoped to the connection. The user is the *root identity* --- they can do
anything: spawn agents, modify settings, grant or revoke permissions, read any
audit log, terminate the server, read or write any memory layer or config file.
Root access is identity, not a permission grant.

== Agents

An agent is defined by a YAML configuration file on disk and invoked by the
system via Requests. An agent has a role, model assignments, feature flags,
clearance, and permissions. Agents do not authenticate --- they are
instantiated by the server from their file definitions.

== Shared Attributes

Both users and agents:

- Participate in rooms as message senders and recipients.
- Are subject to room protocols (Request ordering, turn limits).
- Have a clearance level that governs data visibility and context assembly.
- Have permissions that govern actions (though users hold implicit root).
- Appear in room transcripts with their identity as the sender.

== Differences

#table(
  columns: (auto, 1fr, 1fr),
  table.header([*Concern*], [*User*], [*Agent*]),
  [Authentication],
  [Credentials],
  [Definition file + Request invocation],

  [Authority],
  [Root (implicit)],
  [Granted permissions],

  [Memory ownership],
  [N/A (user is the subject of memory)],
  [Per-agent clearance-stratified files],

  [Clearance],
  [Implicitly top-tier],
  [Configured per-agent],

  [Lifecycle],
  [Login/logout],
  [Persistent or ephemeral],

  [Invocation],
  [Self-directed],
  [Protocol issues a Request],
)

This unification means the frontend treats DMs with agents and multi-participant
rooms identically. A DM with the housewife is just a room with two
participants. A structured debate room with three agents is the same primitive
with a different protocol.
