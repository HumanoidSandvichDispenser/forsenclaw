<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue';

import { getAgentDag } from '@/client';
import type { DagNode } from '@/client';
import BaseModal from '@/components/BaseModal.vue';
import { useWebSocket } from '@/composables/useWebSocket';
import type { DagUpdatePayload } from '@/composables/useWebSocket';
import { useClientStore } from '@/stores/client';

const props = defineProps<{
  open: boolean;
  agentName: string;
}>();

const emit = defineEmits<{
  (e: 'close'): void;
}>();

const clientStore = useClientStore();
const ws = useWebSocket();

const loading = ref(false);
const error = ref('');
// Nodes keyed by id, in insertion order, so live updates merge in place while
// new nodes append.
const nodes = ref<DagNode[]>([]);

let unsubEvent: (() => void) | null = null;

async function loadSnapshot() {
  loading.value = true;
  error.value = '';
  nodes.value = [];
  try {
    const res = await getAgentDag({
      client: clientStore.client,
      path: { agent_name: props.agentName },
    });
    const body = (res as any)?.data ?? res;
    nodes.value = body?.nodes ?? [];
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'failed to load DAG';
  } finally {
    loading.value = false;
  }
}

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
}

function start() {
  loadSnapshot();
  ws.connect();
  ws.subscribeAgent(props.agentName);
  unsubEvent = ws.onEvent((event) => {
    if (event.type === 'dag.update') {
      mergeNode(event.payload as DagUpdatePayload);
    }
  });
}

function stop() {
  if (unsubEvent) {
    unsubEvent();
    unsubEvent = null;
  }
  ws.unsubscribeAgent(props.agentName);
  ws.disconnect();
}

// Re-bind whenever the modal opens or the target agent changes.
watch(
  () => [props.open, props.agentName] as const,
  ([open], prev) => {
    const wasOpen = prev?.[0];
    if (open) {
      if (wasOpen) stop();
      start();
    } else if (wasOpen) {
      stop();
    }
  },
  { immediate: true },
);

const liveCount = computed(
  () => nodes.value.filter((n) => n.state !== 'resolved' && n.state !== 'failed').length,
);

// now is a reactive clock driving the live elapsed timers of in-flight nodes.
// It only ticks while something is unsettled, so a fully resolved DAG does no
// repeated work.
const now = ref(Date.now());
let tick: ReturnType<typeof setInterval> | null = null;

function startTick() {
  if (tick) return;
  tick = setInterval(() => {
    now.value = Date.now();
  }, 100);
}

function stopTick() {
  if (tick) {
    clearInterval(tick);
    tick = null;
  }
}

// Tick only while the modal is open and something is actually in flight.
const ticking = computed(() => props.open && liveCount.value > 0);
watch(
  ticking,
  (on) => {
    if (on) startTick();
    else stopTick();
  },
  { immediate: true },
);

onBeforeUnmount(() => {
  stop();
  stopTick();
});

function fmtKind(kind?: string): string {
  return kind && kind.length ? kind : 'node';
}

// Elapsed renders the node's lifetime: settled duration if finished, else the
// time since it started (ticking live off `now`), else a dash for not-yet-
// started nodes.
function elapsed(n: DagNode): string {
  if (!n.started_at) return '—';
  const start = Date.parse(n.started_at);
  const end = n.settled_at ? Date.parse(n.settled_at) : now.value;
  const ms = end - start;
  if (ms < 1000) return `${ms} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}
</script>

<template>
  <BaseModal :open="open" label="Agent request DAG" width="44rem" @close="emit('close')">
    <template #header>
      <h2 class="h2">Request DAG</h2>
      <div class="meta">{{ agentName }}</div>
      <div v-if="nodes.length" class="meta stats">
        <span>{{ nodes.length }} nodes</span>
        &middot;
        <span>{{ liveCount }} in flight</span>
      </div>
    </template>

    <div class="body">
      <p v-if="loading" class="muted">Loading DAG…</p>
      <p v-else-if="error" class="error">{{ error }}</p>
      <p v-else-if="!nodes.length" class="muted">Agent idle — no request DAG.</p>

      <ul v-else class="nodes">
        <li v-for="n in nodes" :key="n.id" class="node" :class="`state-${n.state}`">
          <div class="node-head">
            <span class="kind">{{ fmtKind(n.kind) }}</span>
            <span class="label">{{ n.label || n.id }}</span>
            <span class="state">{{ n.state }}</span>
            <span class="elapsed">{{ elapsed(n) }}</span>
          </div>
          <div v-if="n.waiting_on" class="waiting">waiting on: {{ n.waiting_on }}</div>
          <div v-if="n.blocked_by && n.blocked_by.length" class="edges">
            blocked by:
            <code v-for="b in n.blocked_by" :key="b">{{ b }}</code>
          </div>
          <div v-if="n.children && n.children.length" class="edges">
            children:
            <code v-for="c in n.children" :key="c">{{ c }}</code>
          </div>
        </li>
      </ul>
    </div>
  </BaseModal>
</template>

<style scoped>
.h2 {
  margin: 0;
}

.meta {
  margin: 0.25rem 0 0;
  color: var(--fg-tertiary);
  font-size: var(--body-xs-size);
}

.stats {
  font-family: var(--code-family);
}

.body {
  padding: 1rem;
}

.muted {
  color: var(--fg-tertiary);
}

.error {
  color: var(--error);
}

.nodes {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 0.5rem;
}

.node {
  border: 1px solid var(--border-subtle);
  border-left: 3px solid var(--border-default);
  border-radius: 0.5rem;
  background: var(--bg-primary);
  padding: 0.5rem 0.7rem;
}

/* State accents on the left border so in-flight vs settled reads at a glance. */
.node.state-in_progress {
  border-left-color: var(--accent-primary);
}

.node.state-blocked {
  border-left-color: var(--warning);
}

.node.state-resolved {
  border-left-color: var(--success);
}

.node.state-failed {
  border-left-color: var(--error);
}

.node-head {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.kind {
  text-transform: uppercase;
  letter-spacing: 0.04em;
  font-size: 0.62rem;
  font-weight: 700;
  padding: 0.1rem 0.4rem;
  border-radius: 0.35rem;
  background: var(--bg-tertiary);
  color: var(--fg-secondary);
}

.label {
  font-family: var(--code-family);
  font-size: var(--body-sm-size);
  color: var(--fg-primary);
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.state {
  font-size: var(--body-xs-size);
  color: var(--fg-tertiary);
}

.elapsed {
  font-family: var(--code-family);
  font-size: var(--body-xs-size);
  color: var(--fg-tertiary);
}

.waiting {
  margin-top: 0.3rem;
  font-size: var(--body-xs-size);
  color: var(--warning);
}

.edges {
  margin-top: 0.25rem;
  font-size: var(--body-xs-size);
  color: var(--fg-tertiary);
  display: flex;
  flex-wrap: wrap;
  gap: 0.3rem;
  align-items: center;
}

.edges code {
  font-size: 0.7rem;
}
</style>
