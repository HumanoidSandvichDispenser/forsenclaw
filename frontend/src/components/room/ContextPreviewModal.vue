<script setup lang="ts">
import { ref, watch } from 'vue';

import { previewContext } from '@/client';
import type { GetContextPreviewResponseBody } from '@/client';
import { useClientStore } from '@/stores/client';

const props = defineProps<{
  open: boolean;
  roomId: string;
  agentName: string;
}>();

const emit = defineEmits<{
  (e: 'close'): void;
}>();

const clientStore = useClientStore();
const loading = ref(false);
const error = ref('');
const data = ref<GetContextPreviewResponseBody | null>(null);

async function load() {
  loading.value = true;
  error.value = '';
  data.value = null;
  try {
    const res = await previewContext({
      client: clientStore.client,
      path: { room_id: props.roomId, agent_name: props.agentName },
    } as any);
    const body = (res as any)?.data ?? res;
    data.value = body as GetContextPreviewResponseBody;
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'failed to load preview';
  } finally {
    loading.value = false;
  }
}

// Fetch fresh whenever the modal opens for an agent.
watch(
  () => [props.open, props.agentName] as const,
  ([open]) => {
    if (open) load();
  },
  { immediate: true },
);

function onClose() {
  emit('close');
}

function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="overlay" @click.self="onClose">
      <div class="modal" role="dialog" aria-modal="true" aria-label="Context preview">
        <header class="header">
          <div>
            <h2 class="h2">Context Preview</h2>
            <div class="meta">{{ agentName }}</div>
            <div v-if="data" class="meta stats">
              <span>{{ data.messages?.length ?? 0 }} messages</span>
              &middot;
              <span>{{ data.tools?.length ?? 0 }} tools</span>
              &middot;
              <span>{{ fmtBytes(data.assembled_bytes) }}</span>
              &middot;
              <span>compaction @ {{ data.compaction_offset }}</span>
            </div>
          </div>
          <button class="icon-btn" type="button" @click="onClose" aria-label="Close">✕</button>
        </header>

        <div class="body">
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
                </div>
                <pre class="msg-content">{{ m.content }}</pre>
              </div>
            </section>
          </template>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.overlay {
  position: fixed;
  inset: 0;
  background: var(--overlay-scrim);
  display: grid;
  place-items: center;
  padding: 1rem;
  z-index: 50;
}

.modal {
  width: min(52rem, 100%);
  max-height: min(85vh, 56rem);
  background: var(--bg-elevated);
  border: 1px solid var(--border-subtle);
  border-radius: 0.75rem;
  box-shadow: 0 18px 60px rgba(0, 0, 0, 0.15);
  overflow: hidden;
  display: grid;
  grid-template-rows: auto 1fr;
}

.header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
  padding: 1rem;
  border-bottom: 1px solid var(--border-subtle);
}

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

.icon-btn {
  padding: 0.25rem 0.5rem;
  border: 1px solid var(--border-default);
  border-radius: 0.5rem;
  background: var(--bg-primary);
}

.body {
  overflow: auto;
  padding: 1rem;
  display: grid;
  gap: 1.25rem;
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
  background: var(--bg-tertiary, #222);
  color: var(--fg-secondary);
}

.role-system {
  background: color-mix(in srgb, var(--accent, #4a9eff) 18%, transparent);
  color: var(--accent, #4a9eff);
}

.role-assistant {
  background: color-mix(in srgb, var(--warning, #e0a155) 18%, transparent);
  color: var(--warning, #e0a155);
}

.msg-name {
  color: var(--fg-tertiary);
  font-size: var(--body-xs-size);
  font-family: var(--code-family);
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
}

.error {
  color: var(--error, #e05555);
}
</style>
