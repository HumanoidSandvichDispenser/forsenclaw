<script setup lang="ts">
import { computed, ref } from 'vue';

import type { ConfirmationResponse } from '@/client/types.gen';
import { useConfirmationsStore } from '@/stores/confirmations';

const props = defineProps<{
  roomId: string;
  confirmation: ConfirmationResponse;
}>();

// Maps the policy reason to UI treatment. Only blp_write_down and
// permission_confirmation actually reach a confirmation; deny/allow reasons
// never produce a banner. Unknown reasons fall back to a neutral label.
const reasonMeta = computed(() => {
  switch (props.confirmation.reason) {
    case 'blp_write_down':
      return {
        kind: 'declass',
        label: 'Declassification',
        detail: 'Moves classified context into a lower-clearance room.',
      };
    case 'permission_confirmation':
      return {
        kind: 'permission',
        label: 'Permission required',
        detail: 'This tool needs your approval before it runs.',
      };
    default:
      return {
        kind: 'permission',
        label: 'Confirmation required',
        detail: '',
      };
  }
});

const store = useConfirmationsStore();
const busy = ref(false);
const showRevise = ref(false);
const feedback = ref('');
const editingArgs = ref(false);

const formattedArgs = (() => {
  try {
    return JSON.stringify(JSON.parse(props.confirmation.args), null, 2);
  } catch {
    return props.confirmation.args;
  }
})();

const editedArgs = ref(formattedArgs);
const argsError = ref('');

function toggleEditArgs() {
  editingArgs.value = !editingArgs.value;
  argsError.value = '';
}

async function act(action: 'allow' | 'deny' | 'revise') {
  if (busy.value) return;
  if (action === 'allow' && editingArgs.value) {
    try {
      JSON.parse(editedArgs.value);
    } catch {
      argsError.value = 'invalid JSON';
      return;
    }
  }
  busy.value = true;
  try {
    await store.respond(props.roomId, props.confirmation.node_id, action, {
      args: action === 'allow' && editingArgs.value ? editedArgs.value : undefined,
      feedback: action === 'revise' ? feedback.value : undefined,
    });
  } finally {
    busy.value = false;
    showRevise.value = false;
  }
}
</script>

<template>
  <div class="confirmation-banner" :class="`is-${reasonMeta.kind}`">
    <div class="confirmation-header">
      <span class="reason-badge">{{ reasonMeta.label }}</span>
      <span class="agent-name">{{ confirmation.agent_name }}</span>
      <span class="wants-to">wants to call</span>
      <code class="tool-name">{{ confirmation.tool_name }}</code>
    </div>
    <p v-if="reasonMeta.detail" class="reason-detail">{{ reasonMeta.detail }}</p>

    <textarea v-if="editingArgs" v-model="editedArgs" class="args args-edit" spellcheck="false" />
    <pre v-else class="args">{{ formattedArgs }}</pre>
    <span v-if="argsError" class="args-error">{{ argsError }}</span>

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
      <button class="btn btn-edit" :disabled="busy" @click="toggleEditArgs">{{ editingArgs ? 'Lock' : 'Edit' }}</button>
      <button class="btn btn-deny" :disabled="busy" @click="act('deny')">Deny</button>
    </div>
  </div>
</template>

<style scoped>
.confirmation-banner {
  border: 1px solid var(--border, #333);
  border-left: 3px solid var(--border, #333);
  border-radius: 8px;
  padding: 0.75rem 1rem;
  background: var(--bg-secondary, #1a1a1a);
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

/* Declassification is security-significant — flag it distinctly. */
.confirmation-banner.is-declass {
  border-left-color: var(--warning, #e0a155);
}

.confirmation-header {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  font-size: 0.9rem;
  flex-wrap: wrap;
}

.reason-badge {
  text-transform: uppercase;
  letter-spacing: 0.04em;
  font-size: 0.7rem;
  font-weight: 600;
  padding: 0.1rem 0.4rem;
  border-radius: 4px;
  background: var(--bg-tertiary, #222);
  color: var(--fg-tertiary, #888);
}

.is-declass .reason-badge {
  background: color-mix(in srgb, var(--warning, #e0a155) 18%, transparent);
  color: var(--warning, #e0a155);
}

.reason-detail {
  margin: 0;
  font-size: 0.8rem;
  color: var(--fg-secondary, #aaa);
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

.btn-edit {
  background: var(--bg-tertiary, #333);
  color: inherit;
}

.args-edit {
  width: 100%;
  min-height: 6rem;
  resize: vertical;
  font-family: monospace;
  font-size: 0.8rem;
  color: var(--fg-secondary, #aaa);
  background: var(--bg-tertiary, #222);
  border: 1px solid var(--accent, #4a9eff);
  border-radius: 4px;
  padding: 0.5rem;
  box-sizing: border-box;
}

.args-error {
  font-size: 0.8rem;
  color: var(--error, #e05555);
}
</style>
