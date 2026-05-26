= Storage Architecture

forsenClaw follows the XDG Base Directory Specification. All paths respect
environment variable overrides (`$XDG_CONFIG_HOME`, `$XDG_DATA_HOME`,
`$XDG_CACHE_HOME`).

== Directory Layout

```
$XDG_CONFIG_HOME/forsenClaw/              # Config — small, version-controllable
├── hearth.yaml                           # Server config: providers, models, listen addr
├── policy.rego                           # OPA policy file (ABAC rules)
└── agents/
    ├── housewife/
    │   └── agent.yaml                    # Role, models, flags, permissions, clearance
    └── scout/
        └── agent.yaml

$XDG_DATA_HOME/forsenClaw/               # Data — large, persistent
├── agents/
│   ├── housewife/
│   │   ├── MEMORY-1.md                  # Public-facing facts (clearance 1)
│   │   ├── MEMORY-2.md                  # External-safe facts (clearance 2)
│   │   ├── MEMORY-3.md                  # Professional context (clearance 3)
│   │   ├── MEMORY-4.md                  # Full personal context (clearance 4)
│   │   ├── MEMORY-5.md                  # Vault (clearance 5, manual only)
│   │   ├── DREAMS.md                    # Dreaming activity log (optional)
│   │   └── memory/
│   │       ├── clearance-2/
│   │       │   └── 2026-05-23.md
│   │       ├── clearance-4/
│   │       │   └── 2026-05-23.md
│   │       └── clearance-5/
│   │           └── 2026-05-23.md
│   └── scout/
│       ├── MEMORY-1.md
│       ├── MEMORY-2.md
│       └── memory/
│           └── clearance-2/
│               └── 2026-05-23.md
├── rooms/
│   ├── <room-id>.jsonl                  # Room transcripts
│   └── ...
├── staging/                             # Pending config and policy proposals
│   └── agents/
│       └── housewife/
│           └── agent.yaml.proposed
└── db/
    ├── rooms.db                         # Room metadata, protocol state, compaction cursors
    └── audit.db                         # Audit log

$XDG_CACHE_HOME/forsenClaw/             # Cache — rebuildable
├── search.db                           # Hybrid search index
└── embeddings/                         # Cached embedding vectors
```

== Server Configuration

`hearth.yaml` includes a `context:` block:

```yaml
context:
  current_room_window: 50
  other_room_window: 10
  compaction_trigger: 524288
  compaction_target: 262144
  minimum_guaranteed: 20
```

- `current_room_window`: guaranteed minimum tail size for the active room.
- `other_room_window`: number of recent messages read from each other room.
- `compaction_trigger`: assembled-context byte threshold that triggers
  compaction.
- `compaction_target`: desired size after compaction.
- `minimum_guaranteed`: hard floor that compaction never crosses.

Invalid values are rejected at startup. In particular, `compaction_target`
must be lower than `compaction_trigger`.

Clearance level labels are also defined in `hearth.yaml`:

```yaml
clearance_levels:
  - level: 1
    label: public
    description: "Safe to expose anywhere."
  - level: 2
    label: external
    description: "Safe for external integrations."
  - level: 3
    label: professional
    description: "Task-scoped context."
  - level: 4
    label: personal
    description: "Full personal context."
  - level: 5
    label: private
    description: "Vault tier. Manual curation only."
```

Tool servers are configured in `hearth.yaml` under the `tools:` block. Each
server (and built-in tool) is assigned a clearance level that classifies the
sensitivity of the data it handles. Omitting `clearance` defaults to the system
maximum --- conservative, since unlabeled tools are assumed to handle the most
sensitive data.

```yaml
tools:
  max_tool_iterations: 10
  webfetch:
    clearance: 1                # public data
  brave_search:
    api_key: "${BRAVE_API_KEY}"
    clearance: 1                # public data
  servers:
    - name: email
      url: "https://..."
      clearance: 2              # external — handles outbound data
    - name: calendar
      url: "https://..."
      clearance: 3              # professional
    - name: finances
      url: "https://..."
      clearance: 5              # private — vault-tier data
```

Clearance on a tool server applies to all tools exposed by that server. If
finer-grained control is needed per tool within a server, the server should be
split into multiple instances with different clearance levels (see @mcp).

== What Lives Where and Why

#table(
  columns: (auto, auto, 1fr),
  table.header([*Data*], [*Location*], [*Rationale*]),
  [Agent definitions], [Config],
  [Identity. Version-controllable. Mountable in Docker.],
  [OPA policy], [Config],
  [Git-trackable, diffable, human-editable. Proposed via staging.],
  [Server config], [Config],
  [Providers, models, clearance labels, tool clearance. Rarely changes.],
  [Agent memory (MEMORY-k.md)], [Data],
  [Agent-written, grows over time, persistent.],
  [Daily notes], [Data],
  [Clearance-stratified; grows over time.],
  [Room transcripts], [Data],
  [Append-only conversation records.],
  [Config proposals], [Data],
  [Staging area for pending changes. Not yet config.],
  [Room metadata], [Data (SQLite)],
  [Dynamic, queryable (list rooms, filter by participant).],
  [Compaction cursors], [Data (SQLite)],
  [Per-agent, per-room `compacted_offset`.],
  [Request DAG], [Data (SQLite)],
  [Node and edge state for in-flight Requests.],
  [Audit log], [Data (SQLite)],
  [Structured queries, append-only guarantees, DAG traces.],
  [Search index], [Cache],
  [Rebuildable from files. Excludable from backups.],
  [Embeddings], [Cache],
  [Rebuildable. Expensive to recompute but not critical.],
)

== SQLite Schemas

=== Compaction Cursors (rooms.db)

```sql
CREATE TABLE IF NOT EXISTS compaction_cursors (
  agent_name TEXT NOT NULL,
  room_id    TEXT NOT NULL,
  compacted_offset INTEGER NOT NULL DEFAULT 0,
  updated_at DATETIME NOT NULL,
  PRIMARY KEY (agent_name, room_id)
);
```

=== Request DAG (rooms.db)

```sql
CREATE TABLE IF NOT EXISTS request_nodes (
  id        TEXT PRIMARY KEY,
  room_id   TEXT,
  target    TEXT NOT NULL,
  source    TEXT NOT NULL,  -- room | system | event
  state     TEXT NOT NULL,  -- pending | in_progress | blocked | resolved | failed
  parent_id TEXT,
  payload   TEXT NOT NULL,  -- JSON
  created_at DATETIME NOT NULL,
  resolved_at DATETIME
);

CREATE TABLE IF NOT EXISTS request_edges (
  parent_id TEXT NOT NULL,
  child_id  TEXT NOT NULL,
  PRIMARY KEY (parent_id, child_id),
  FOREIGN KEY (parent_id) REFERENCES request_nodes(id),
  FOREIGN KEY (child_id)  REFERENCES request_nodes(id)
);
```

== Version Control

`$XDG_CONFIG_HOME/forsenClaw/` can be configured as a git repo containing
agent definitions, server config, and the OPA policy file.
`$XDG_DATA_HOME/forsenClaw/agents/` is optionally a second repo for memory
history. Room transcripts can be included or excluded; they are append-only so
git handles them, but they grow.

The `db/` directory is excluded from version control. SQLite databases are
queryable runtime state; audit can be exported if archival is needed.

== Docker Volume Mapping

```yaml
volumes:
  - forsenClaw_config:/home/user/.config/forsenClaw   # Small, version-controllable
  - forsenClaw_data:/home/user/.local/share/forsenClaw # Large, persistent
  # cache intentionally omitted — rebuilt on start
```

Config volume can be mounted read-only to prevent agent self-modification
entirely (overriding any `config:write` permissions). This is a
deployment-level decision.
