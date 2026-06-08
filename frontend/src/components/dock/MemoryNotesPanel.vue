<script setup lang="ts">
import { computed, ref, toRef } from 'vue';

import { renderMarkdown } from '@/utils/markdown';
import { useAgentMemory } from '@/composables/useAgentMemory';
import CompactModal from '@/components/dock/CompactModal.vue';

// Memory & daily-notes inspection panel. Shows the agent's memory broken out by
// clearance level (only the levels the viewer is cleared to read — the backend
// gates the read) as collapsible drawers, plus the room's compaction stats. The
// Compact button compacts this agent's transcript in this room; the resulting
// summary lands in the daily notes, so we refresh after.
const props = defineProps<{ agentName?: string; roomId?: number }>();

const agentName = toRef(props, 'agentName');
const roomId = toRef(props, 'roomId');

const { levels, stats, loading, error, forbidden, compacting, compact } = useAgentMemory(
  computed(() => agentName.value ?? ''),
  computed(() => roomId.value ?? 0),
);

// A clearance-0 level is the legacy flat (unleveled) baseline.
function levelLabel(clearance: number): string {
  return clearance === 0 ? 'base' : `C${clearance}`;
}

function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

// Rough token estimate; transcripts are English-ish prose, ~4 bytes/token.
function fmtTokens(bytes: number): string {
  const t = Math.round(bytes / 4);
  if (t < 1000) return `${t}`;
  return `${(t / 1000).toFixed(1)}k`;
}

// The Compact button opens a modal for target size + additional instructions.
const compactOpen = ref(false);

// The configured automatic target, in KB, for the modal's target placeholder.
const autoTargetKb = computed(() =>
  stats.value?.target ? String(Math.round(stats.value.target / 1024)) : '',
);

async function runCompact(payload: { targetBytes?: number; instructions?: string }) {
  await compact(payload.targetBytes, payload.instructions);
  compactOpen.value = false;
}
</script>

<template>
  <div class="memory-panel">
    <div class="sub">
      <span v-if="agentName" class="agent">{{ agentName }}</span>
    </div>

    <div v-if="stats" class="stats">
      <span>{{ stats.messages }} live msgs</span>
      &middot;
      <span>{{ fmtBytes(stats.bytes) }}</span>
      &middot;
      <span>~{{ fmtTokens(stats.bytes) }} tok</span>
      &middot;
      <span>cursor @ {{ stats.offset }}</span>
      <span v-if="stats.target" class="target">
        target {{ fmtBytes(stats.target) }}<template v-if="stats.trigger"> · trigger {{ fmtBytes(stats.trigger) }}</template>
      </span>
    </div>

    <div class="compact-row">
      <button
        class="compact-btn"
        type="button"
        :disabled="compacting || !roomId"
        title="Compact this agent's transcript in this room"
        @click="compactOpen = true"
      >
        {{ compacting ? 'Compacting…' : 'Compact…' }}
      </button>
    </div>

    <CompactModal
      :open="compactOpen"
      :agent-name="agentName"
      :auto-target-kb="autoTargetKb"
      :compacting="compacting"
      @close="compactOpen = false"
      @submit="runCompact"
    />

    <p v-if="loading" class="muted">Loading memory…</p>
    <p v-else-if="forbidden" class="muted">Not cleared to view this agent’s memory.</p>
    <p v-else-if="error" class="error">{{ error }}</p>
    <p v-else-if="!levels.length" class="muted">No memory or notes yet.</p>

    <div v-else class="levels">
      <details v-for="(lvl, i) in levels" :key="lvl.clearance" class="level" :open="i === 0">
        <summary class="level-head">
          <span class="badge" :class="{ base: lvl.clearance === 0 }">{{ levelLabel(lvl.clearance) }}</span>
          <span class="level-meta">
            <template v-if="lvl.memory">memory</template>
            <template v-if="lvl.memory && lvl.notes?.length"> · </template>
            <template v-if="lvl.notes?.length">{{ lvl.notes.length }} note(s)</template>
          </span>
        </summary>

        <div class="level-body">
          <details v-if="lvl.memory" class="drawer" open>
            <summary class="drawer-head">Memory</summary>
            <div class="md" v-html="renderMarkdown(lvl.memory)" />
          </details>

          <details v-if="lvl.notes && lvl.notes.length" class="drawer">
            <summary class="drawer-head">Daily notes</summary>
            <div v-for="note in lvl.notes" :key="note.date" class="note">
              <span class="note-date">{{ note.date }}</span>
              <div class="md" v-html="renderMarkdown(note.content)" />
            </div>
          </details>
        </div>
      </details>
    </div>
  </div>
</template>

<style scoped>
.memory-panel {
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
}

.sub {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: var(--body-xs-size);
  color: var(--fg-tertiary);
}

.stats {
  font-family: var(--code-family);
  font-size: var(--body-xs-size);
  color: var(--fg-tertiary);
  line-height: 1.5;
}

.stats .target {
  display: block;
  color: var(--fg-muted);
}

.compact-row {
  display: flex;
  gap: 0.4rem;
}

.compact-btn {
  flex: 1 1 auto;
}

.compact-btn:disabled {
  opacity: 0.5;
  cursor: default;
}

.muted {
  color: var(--fg-tertiary);
  margin: 0;
}

.error {
  color: var(--error);
  margin: 0;
}

.levels {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.level {
  border: 1px solid var(--border-subtle);
  border-radius: 0.5rem;
  background: var(--bg-primary);
  overflow: hidden;
}

.level-head {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.45rem 0.6rem;
  cursor: pointer;
  list-style: none;
  user-select: none;
}

.level-head::-webkit-details-marker {
  display: none;
}

.level[open] .level-head {
  border-bottom: 1px solid var(--border-subtle);
}

.level-meta {
  font-size: var(--body-xs-size);
  color: var(--fg-tertiary);
}

.badge {
  display: inline-block;
  font-family: var(--code-family);
  font-size: var(--body-xs-size);
  padding: 0.1rem 0.4rem;
  border-radius: 0.3rem;
  background: var(--bg-tertiary);
  color: var(--fg-secondary);
}

.badge.base {
  color: var(--fg-tertiary);
}

.level-body {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
  padding: 0.5rem 0.6rem;
}

.drawer {
  border-top: 1px solid var(--border-subtle);
  padding-top: 0.4rem;
}

.drawer:first-child {
  border-top: none;
  padding-top: 0;
}

.drawer-head {
  font-size: var(--body-xs-size);
  font-weight: var(--weight-medium);
  color: var(--fg-tertiary);
  text-transform: uppercase;
  letter-spacing: 0.04em;
  cursor: pointer;
  user-select: none;
  list-style: none;
}

.drawer-head::-webkit-details-marker {
  display: none;
}

.note {
  margin: 0.4rem 0;
}

.note-date {
  font-family: var(--code-family);
  font-size: var(--body-xs-size);
  color: var(--fg-muted);
}

.md {
  font-size: var(--body-sm-size);
  color: var(--fg-primary);
  word-break: break-word;
  margin-top: 0.3rem;
}
</style>
