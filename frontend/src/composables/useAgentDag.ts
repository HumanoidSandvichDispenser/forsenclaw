import { computed, ref } from 'vue';

import { getAgentDag } from '@/client';
import type { DagNode } from '@/client';
import { useClientStore } from '@/stores/client';
import type { DagUpdatePayload, useWebSocket } from '@/composables/useWebSocket';

// Shared, single-focus DAG state. A room watches one agent at a time, so the
// state lives at module scope (like useDock) — the room binds it, the dock panel
// reads it. Keeping it here rather than inside the panel lets the rail's live dot
// reflect in-flight nodes even while the panel is collapsed, since the
// subscription outlives the panel's mount.

type WS = ReturnType<typeof useWebSocket>;

const nodes = ref<DagNode[]>([]);
const loading = ref(false);
const error = ref('');
// forbidden is the clearance gate firing (403): the viewer sits below the
// agent's clearance. Kept distinct from error so the panel can show a calm "not
// cleared" state rather than a failure.
const forbidden = ref(false);

const liveCount = computed(
  () => nodes.value.filter((n) => n.state !== 'resolved' && n.state !== 'failed').length,
);

// now is a reactive clock for live elapsed timers. It ticks only while a node is
// unsettled, so a resolved (or empty) DAG does no repeated work.
const now = ref(Date.now());
let tick: ReturnType<typeof setInterval> | null = null;
function syncTick() {
  const want = liveCount.value > 0;
  if (want && !tick) {
    tick = setInterval(() => {
      now.value = Date.now();
    }, 100);
  } else if (!want && tick) {
    clearInterval(tick);
    tick = null;
  }
}

let boundAgent = '';
let boundWs: WS | null = null;
let unsubEvent: (() => void) | null = null;

// mergeNode applies a streamed transition: replace the node if present (keeping
// its position), otherwise append it.
function mergeNode(node: DagUpdatePayload) {
  const idx = nodes.value.findIndex((n) => n.id === node.id);
  if (idx === -1) {
    nodes.value = [...nodes.value, node];
  } else {
    const next = nodes.value.slice();
    next[idx] = node;
    nodes.value = next;
  }
  syncTick();
}

async function loadSnapshot(agentName: string) {
  loading.value = true;
  error.value = '';
  forbidden.value = false;
  nodes.value = [];
  try {
    const res = (await getAgentDag({
      client: useClientStore().client,
      path: { agent_name: agentName },
    })) as { data?: { nodes?: DagNode[] }; response?: Response };
    if (res?.response?.status === 403) {
      forbidden.value = true;
      return;
    }
    nodes.value = res?.data?.nodes ?? [];
  } catch (e) {
    const status =
      (e as { response?: { status?: number }; status?: number })?.response?.status ??
      (e as { status?: number })?.status;
    if (status === 403) {
      forbidden.value = true;
    } else {
      error.value = e instanceof Error ? e.message : 'failed to load DAG';
    }
  } finally {
    loading.value = false;
    syncTick();
  }
}

// bindAgentDag points the shared state at one agent over the given (already
// connected) room socket. Pass an empty name to unbind. Re-binding the same
// agent on the same socket is a no-op, so repeated room renders don't thrash the
// subscription.
export function bindAgentDag(ws: WS, agentName: string) {
  if (agentName === boundAgent && ws === boundWs) return;

  // Tear down the previous binding.
  if (unsubEvent) {
    unsubEvent();
    unsubEvent = null;
  }
  if (boundWs && boundAgent) boundWs.unsubscribeAgent(boundAgent);

  boundWs = agentName ? ws : null;
  boundAgent = agentName;
  nodes.value = [];
  error.value = '';
  forbidden.value = false;
  syncTick();

  if (!agentName) return;

  ws.subscribeAgent(agentName);
  unsubEvent = ws.onEvent((event) => {
    if (event.type === 'dag.update') {
      mergeNode(event.payload as DagUpdatePayload);
    }
  });
  loadSnapshot(agentName);
}

function fmtKind(kind?: string): string {
  return kind && kind.length ? kind : 'node';
}

// elapsed renders a node's lifetime: settled duration if finished, else the time
// since it started (ticking live off `now`), else a dash for not-yet-started.
function elapsed(n: DagNode): string {
  if (!n.started_at) return '—';
  const start = Date.parse(n.started_at);
  const end = n.settled_at ? Date.parse(n.settled_at) : now.value;
  const ms = end - start;
  if (ms < 1000) return `${ms} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}

export function useAgentDag() {
  return { nodes, loading, error, forbidden, liveCount, elapsed, fmtKind };
}
