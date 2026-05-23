#import "@preview/grape-suite:4.0.0": exercise
#import exercise: project, task, subtask

#show: project.with(
  title: "forsenClaw Design Document v5",
  abstract: [
    A self-hosted multi-agent orchestration system with file-based agent
    definitions, clearance-stratified memory, OPA-based access control,
    and pub/sub-driven rooms. Runs on a NAS or similar always-on host.
  ],
)

#outline(title: "Contents", depth: 3, indent: 1em)

#pagebreak()

#include "sections/overview.typ"
#include "sections/actors.typ"
#include "sections/agents.typ"
#include "sections/memory.typ"
#include "sections/access-control.typ"
#include "sections/rooms.typ"
#include "sections/lifecycle.typ"
#include "sections/storage.typ"
#include "sections/proactive.typ"
#include "sections/mcp.typ"
#include "sections/frontend.typ"
#include "sections/appendix.typ"
