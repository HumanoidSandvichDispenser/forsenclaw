# Architecture (C4 / LikeC4)

C4 model of **forsenClaw**, written in [LikeC4](https://likec4.dev).

The model is split across three files:

| File | Holds |
|------|-------|
| `specification.c4` | Element kinds (person / system / external / container / component) and their styling |
| `model.c4` | The single model: elements + all relationships (declared once) |
| `views.c4` | The views below |

Relationships are declared **once** in the model; LikeC4 rolls them up/down, so
the container view stays consistent with the component views automatically.

## Views

| View | Level | What it shows |
|------|-------|---------------|
| `index` | System Context | Who/what forsenClaw touches (user, LLM providers, MCP servers) |
| `containers` | Container | The modules and their dependencies |
| `agentRuntime` | Component | Inside the agent loop — the hard part |
| `mcp` | Component | Tool dispatch — registry + executor |
| `builtinTools` | Component | The in-process MCPClient tools |
| `api` | Component | Inbound routes + outbound WebSocket writers |
| `policy` | Component | Authorization — evaluators and the most-restrictive fold |
| `assembler` | Component | The context-assembly pipeline |
| `store` | Component | Repositories backed by SQLite |
| `audit` | Component | Logger, per-sink filter, sinks |
| `sendMessage` | Dynamic | A user message → agent reply, end to end |
| `inferenceTurn` | Dynamic | One turn: assemble → infer → tools → confirm → write |
| `contextAssembly` | Dynamic | Building a turn's context window |
| `toolAuthorization` | Dynamic | How a tool call is allowed, denied, or gated |
| `auditEvent` | Dynamic | An event flowing to the sinks |

## Running LikeC4

### Docker

Wired into `docker-compose.yml` under the opt-in `docs` profile, so it doesn't
start with the normal dev stack:

```sh
docker compose --profile docs up likec4
```

Open <http://localhost:8081>. Edits to the `*.c4` files hot-reload in the
browser. (Host port 8081 avoids clashing with nginx on 8000.)

### Without Docker

```sh
cd docs/architecture
bun install
bun start
```

## Conventions

- Descriptions say what a thing is for, not what the code does line by line.
- Add a component view when a container grows an internal shape worth zooming
  into; declare relationships in the model and let them roll up.
- The backend is one Go binary. Its subsystems are modeled as separate
  containers so each gets its own zoomable view — a logical decomposition, not
  a deployment topology.
