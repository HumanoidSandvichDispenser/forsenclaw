<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';

import { PhArrowClockwise, PhHash } from '@phosphor-icons/vue';

import { previewContext } from '@/client';
import type { GetContextPreviewResponseBody } from '@/client';
import { useClientStore } from '@/stores/client';

const props = defineProps<{
  roomId: string;
  agentName: string;
}>();

const clientStore = useClientStore();
const loading = ref(false);
const error = ref('');
const data = ref<GetContextPreviewResponseBody | null>(null);

// The preview is assembled server-side through the same clearance-filtering
// assembler the agent's turn uses, so what comes back is already bounded to the
// viewer's clearance — no separate gate to enforce here.
async function load() {
  if (!props.roomId || !props.agentName) return;
  loading.value = true;
  error.value = '';
  data.value = null;
  try {
    const res = (await previewContext({
      client: clientStore.client,
      path: { room_id: Number(props.roomId), agent_name: props.agentName },
    })) as { data?: GetContextPreviewResponseBody };
    data.value = res?.data ?? null;
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'failed to load preview';
  } finally {
    loading.value = false;
  }
}

onMounted(load);

// The panel outlives a single look, so refetch when the room or agent changes.
watch(() => [props.roomId, props.agentName], load);

function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}
</script>

<template>
  <div class="preview-panel">
    <div class="sub">
      <span v-if="agentName" class="agent">{{ agentName }}</span>
      <button
        class="icon-button refresh"
        type="button"
        :disabled="loading"
        @click="load"
        aria-label="Refresh"
      >
        <PhArrowClockwise :size="15" weight="light" />
      </button>
    </div>

    <div v-if="data" class="stats">
      <span>{{ data.messages?.length ?? 0 }} messages</span>
      &middot;
      <span>{{ data.tools?.length ?? 0 }} tools</span>
      &middot;
      <span>{{ fmtBytes(data.assembled_bytes) }}</span>
      &middot;
      <span>compaction @ {{ data.compaction_offset }}</span>
    </div>

    <p v-if="loading" class="muted">Assembling context…</p>
    <p v-else-if="error" class="error">{{ error }}</p>

    <template v-else-if="data">
      <section v-if="data.tools && data.tools.length" class="tools">
        <h3 class="section-title">Tools</h3>
        <div class="tool-list">
          <span v-for="t in data.tools" :key="t.resource" class="tool-chip">
            <code>{{ t.name }}</code>
            <span class="tool-clr">clr {{ t.clearance }}</span>
          </span>
        </div>
      </section>

      <section class="messages">
        <h3 class="section-title">Assembled messages</h3>
        <div v-for="(m, i) in data.messages ?? []" :key="i" class="msg">
          <div class="msg-head">
            <span class="role" :class="`role-${m.role}`">{{ m.role }}</span>
            <span v-if="m.name" class="msg-name">{{ m.name }}</span>
            <span v-if="m.tool_call_id" class="msg-id">
              <PhHash :size="11" weight="bold" />{{ m.tool_call_id }}
            </span>
          </div>
          <pre v-if="m.content" class="msg-content">{{ m.content }}</pre>
          <div v-for="tc in m.tool_calls ?? []" :key="tc.id" class="tool-call">
            <div class="tool-call-head">
              <code class="tool-call-name">{{ tc.name }}</code>
              <span v-if="tc.id" class="msg-id">
                <PhHash :size="11" weight="bold" />{{ tc.id }}
              </span>
            </div>
            <pre class="msg-content">{{ tc.arguments }}</pre>
          </div>
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.preview-panel {
  display: flex;
  flex-direction: column;
  gap: 0.9rem;
}

.sub {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  font-size: var(--body-xs-size);
  color: var(--fg-tertiary);
}

.refresh {
  --icon-button-size: 1.6rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.4rem;
  background: var(--bg-primary);
  color: var(--fg-secondary);
}

.refresh:hover:not(:disabled) {
  background: var(--bg-tertiary);
}

.refresh:disabled {
  opacity: 0.5;
  cursor: default;
}

.stats {
  font-family: var(--code-family);
  font-size: var(--body-xs-size);
  color: var(--fg-tertiary);
}

.section-title {
  margin: 0 0 0.5rem;
  color: var(--fg-tertiary);
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.tool-list {
  display: flex;
  flex-wrap: wrap;
  gap: 0.4rem;
}

.tool-chip {
  display: inline-flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.2rem 0.5rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.5rem;
  background: var(--bg-primary);
  font-size: var(--body-xs-size);
}

.tool-clr {
  color: var(--fg-tertiary);
}

.msg {
  border: 1px solid var(--border-subtle);
  border-radius: 0.5rem;
  background: var(--bg-primary);
  margin-bottom: 0.6rem;
  overflow: hidden;
}

.msg-head {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.4rem 0.6rem;
  border-bottom: 1px solid var(--border-subtle);
}

.role {
  text-transform: uppercase;
  letter-spacing: 0.04em;
  font-size: 0.65rem;
  font-weight: 700;
  padding: 0.1rem 0.4rem;
  border-radius: 0.35rem;
  background: var(--bg-tertiary);
  color: var(--fg-secondary);
}

.role-system {
  background: color-mix(in srgb, var(--accent-primary) 18%, transparent);
  color: var(--accent-primary);
}

.role-assistant {
  background: color-mix(in srgb, var(--warning) 18%, transparent);
  color: var(--warning);
}

.msg-name {
  color: var(--fg-tertiary);
  font-size: var(--body-xs-size);
  font-family: var(--code-family);
}

.msg-id {
  display: inline-flex;
  align-items: center;
  gap: 0.1rem;
  margin-left: auto;
  color: var(--fg-muted);
  font-size: var(--body-xs-size);
  font-family: var(--code-family);
}

.tool-call {
  border-top: 1px solid var(--border-subtle);
}

.tool-call-head {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.4rem 0.6rem;
}

.tool-call-name {
  font-family: var(--code-family);
  font-size: var(--body-sm-size);
  color: var(--warning);
}

.msg-content {
  margin: 0;
  padding: 0.6rem;
  overflow: auto;
  max-height: 18rem;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: var(--code-family);
  font-size: var(--body-sm-size);
  color: var(--fg-primary);
}

.muted {
  color: var(--fg-tertiary);
  margin: 0;
}

.error {
  color: var(--error);
  margin: 0;
}
</style>
