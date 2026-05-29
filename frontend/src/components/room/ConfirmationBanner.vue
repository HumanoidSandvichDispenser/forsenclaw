<script setup lang="ts">
import { ref } from 'vue';

import type { ConfirmationResponse } from '@/client/types.gen';
import { useConfirmationsStore } from '@/stores/confirmations';

const props = defineProps<{
  roomId: string;
  confirmation: ConfirmationResponse;
}>();

const store = useConfirmationsStore();
const busy = ref(false);
const showRevise = ref(false);
const feedback = ref('');

let parsedArgs: Record<string, unknown> | null = null;
try {
  parsedArgs = JSON.parse(props.confirmation.args);
} catch {
  parsedArgs = null;
}

async function act(action: 'allow' | 'deny' | 'revise') {
  if (busy.value) return;
  busy.value = true;
  try {
    await store.respond(props.roomId, props.confirmation.node_id, action, {
      feedback: action === 'revise' ? feedback.value : undefined,
    });
  } finally {
    busy.value = false;
    showRevise.value = false;
  }
}
</script>

<template>
  <div class="confirmation-banner">
    <div class="confirmation-header">
      <span class="agent-name">{{ confirmation.agent_name }}</span>
      <span class="wants-to">wants to call</span>
      <code class="tool-name">{{ confirmation.tool_name }}</code>
    </div>

    <pre v-if="parsedArgs" class="args">{{ JSON.stringify(parsedArgs, null, 2) }}</pre>
    <pre v-else class="args">{{ confirmation.args }}</pre>

    <div v-if="showRevise" class="revise-row">
      <input
        v-model="feedback"
        class="revise-input"
        placeholder="Tell the agent what to change..."
        @keydown.enter.prevent="act('revise')"
        @keydown.escape="showRevise = false"
      />
      <button class="btn btn-confirm" :disabled="busy" @click="act('revise')">Send</button>
      <button class="btn btn-cancel" @click="showRevise = false">Cancel</button>
    </div>

    <div v-else class="actions">
      <button class="btn btn-confirm" :disabled="busy" @click="act('allow')">Allow</button>
      <button class="btn btn-revise" :disabled="busy" @click="showRevise = true">Revise</button>
      <button class="btn btn-deny" :disabled="busy" @click="act('deny')">Deny</button>
    </div>
  </div>
</template>

<style scoped>
.confirmation-banner {
  border: 1px solid var(--border, #333);
  border-radius: 8px;
  padding: 0.75rem 1rem;
  background: var(--bg-secondary, #1a1a1a);
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.confirmation-header {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  font-size: 0.9rem;
}

.agent-name {
  font-weight: 600;
}

.wants-to {
  color: var(--fg-tertiary, #888);
}

.tool-name {
  background: var(--bg-tertiary, #222);
  padding: 0.1rem 0.4rem;
  border-radius: 4px;
  font-size: 0.85rem;
}

.args {
  margin: 0;
  font-size: 0.8rem;
  color: var(--fg-secondary, #aaa);
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 8rem;
  overflow-y: auto;
  background: var(--bg-tertiary, #222);
  border-radius: 4px;
  padding: 0.5rem;
}

.actions {
  display: flex;
  gap: 0.5rem;
}

.revise-row {
  display: flex;
  gap: 0.5rem;
}

.revise-input {
  flex: 1;
  padding: 0.35rem 0.6rem;
  border-radius: 4px;
  border: 1px solid var(--border, #333);
  background: var(--bg-primary, #111);
  color: inherit;
  font-size: 0.9rem;
}

.btn {
  padding: 0.35rem 0.8rem;
  border-radius: 4px;
  border: none;
  cursor: pointer;
  font-size: 0.85rem;
  font-weight: 500;
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-confirm {
  background: var(--accent, #4a9eff);
  color: #fff;
}

.btn-revise {
  background: var(--bg-tertiary, #333);
  color: inherit;
}

.btn-deny {
  background: transparent;
  color: var(--error, #e05555);
  border: 1px solid var(--error, #e05555);
}

.btn-cancel {
  background: transparent;
  color: var(--fg-tertiary, #888);
}
</style>
