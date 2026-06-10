<script setup lang="ts">
import { useAgentDag } from '@/composables/useAgentDag';

// The room binds the shared DAG state (see useAgentDag); this panel only reads
// and renders it. agentName is display-only — a subheading under the dock title.
defineProps<{ agentName?: string }>();

const { nodes, loading, error, forbidden, liveCount, elapsed, fmtKind, cancelNode, cancelling } =
  useAgentDag();
</script>

<template>
  <div class="dag-panel">
    <div v-if="agentName || nodes.length" class="sub">
      <span v-if="agentName" class="agent">{{ agentName }}</span>
      <span v-if="nodes.length" class="stats">
        {{ nodes.length }} nodes &middot; {{ liveCount }} in flight
      </span>
    </div>

    <p v-if="loading" class="muted">Loading DAG…</p>
    <p v-else-if="forbidden" class="muted">Not cleared to view this agent’s request DAG.</p>
    <p v-else-if="error" class="error">{{ error }}</p>
    <p v-else-if="!nodes.length" class="muted">Agent idle — no request DAG.</p>

    <ul v-else class="nodes">
      <li v-for="n in nodes" :key="n.id" class="node" :class="`state-${n.state}`">
        <div class="node-head">
          <span class="kind">{{ fmtKind(n.kind) }}</span>
          <span class="label">{{ n.label || n.id }}</span>
          <span class="state">{{ n.state }}</span>
          <span class="elapsed">{{ elapsed(n) }}</span>
          <button
            v-if="n.state === 'in_progress'"
            class="cancel"
            :disabled="cancelling.has(n.id)"
            :title="`Cancel ${fmtKind(n.kind)}`"
            @click="cancelNode(n.id)"
          >
            {{ cancelling.has(n.id) ? '…' : 'Stop' }}
          </button>
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
</template>

<style scoped>
.dag-panel {
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
}

.sub {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 0.5rem;
  font-size: var(--body-xs-size);
  color: var(--fg-tertiary);
}

.stats {
  font-family: var(--code-family);
}

.muted {
  color: var(--fg-tertiary);
  margin: 0;
}

.error {
  color: var(--error);
  margin: 0;
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

.cancel {
  font-size: var(--body-xs-size);
  font-family: var(--code-family);
  color: var(--error);
  background: transparent;
  border: 1px solid var(--error);
  border-radius: 0.35rem;
  padding: 0.05rem 0.4rem;
  cursor: pointer;
}

.cancel:hover:not(:disabled) {
  background: var(--error);
  color: var(--bg-primary);
}

.cancel:disabled {
  opacity: 0.5;
  cursor: default;
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
