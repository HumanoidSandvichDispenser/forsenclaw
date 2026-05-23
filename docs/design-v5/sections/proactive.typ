= Proactive Action System

Agents with `proactive_triggers=on` participate in a proactive loop. The
proactive system is unified with the general invocation pipeline: all
proactive work is delivered as *event Requests* via pub/sub. There is no
separate invocation path for proactive behavior.

== Pub/Sub and Event Requests

External signals are delivered via a pub/sub bus. Trigger sources subscribe to
topics; when a topic fires, the system issues an event Request to the target
agent.

Event Request context assembly is minimal: system prompt + memory strata +
event payload (see @agents). No cross-room feed, no room history. This keeps
proactive invocations isolated from active sessions.

== Triggers

=== Interrupt Triggers

Event-driven. An external signal generates an event Request:

- Incoming email matching a filter.
- Calendar event approaching a threshold time.
- Webhook from an external service.
- Time threshold (e.g., "it has been 48 hours since last check").

The dispatcher subscribes to the pub/sub bus on behalf of agents with
`proactive_triggers=on`. When a message arrives on a subscribed topic, the
dispatcher constructs an event Request and enqueues it for the target agent.

=== Scheduled Triggers

Agent-driven. At the end of each check, the agent decides when to next run
and writes a scheduled event (cron syntax or duration offset). When the
schedule fires, the system issues an event Request.

Both trigger types arrive at the agent through the same Request queue as room
Requests. The agent's processing pipeline is identical in both cases.

== Risk Tiers

Risk tier is a property of the action's permission definition, not the agent's
judgment. The OPA policy evaluates it; the dispatcher enforces it.

#table(
  columns: (auto, 1fr),
  table.header([*Risk Tier*], [*Policy*]),
  [Low],
  [Cosmetic, reversible, local. Routine model may act directly. OPA: `allow`.],
  [Medium],
  [Observable effects, messages other actors. Primary model required. OPA:
  `allow` with logging.],
  [High],
  [Irreversible or external side effects. OPA: `require_confirmation`. User
  confirmation required before the action proceeds.],
)

High-risk proactive actions create a blocked Request node in the DAG. The block
resolves when the user approves or denies. On approval the action executes; on
denial the Request is marked failed and the agent is notified.

== Confidence Gating

Each proactive decision is annotated with a confidence score by the agent.
Below a configurable threshold, the agent surfaces a suggestion rather than
acting. This is independent of risk tier --- a low-risk, low-confidence action
surfaces as a suggestion rather than executing silently.

== Context Isolation

Event Requests run in isolated context, separate from any active room session.
Each invocation assembles fresh context (memory strata + daily notes + event
payload). Proactive ticks do not extend or contaminate user-facing session
inactivity timers.

== Notification Delivery

Proactive outputs are delivered as messages in the appropriate room, or as
system notifications for high-priority items. There is no separate "inbox"
surface --- the room the agent belongs to (or a default notification room) is
where proactive outputs land. The frontend surfaces unread counts and
notification badges per room.
