<script setup lang="ts">
import { computed, ref, watchEffect } from 'vue';

import { useAgentsStore } from '@/stores/agents';
import { useRoomsStore } from '@/stores/rooms';

const props = defineProps<{
  open: boolean;
}>();

const emit = defineEmits<{
  (e: 'close'): void;
  (e: 'created', roomId: string): void;
}>();

const agentsStore = useAgentsStore();
const roomsStore = useRoomsStore();

const userName = ref('local');
const selectedAgent = ref<string>('');
const submitting = ref(false);
const submitError = ref<string | null>(null);

watchEffect(() => {
  if (!props.open) return;
  if (agentsStore.agents.length === 0 && !agentsStore.loading) agentsStore.fetchAgents();
});

const agentOptions = computed(() =>
  agentsStore.agents
    .filter((a) => a.active)
    .map((a) => ({
      value: a.name,
      label: a.name,
    })),
);

watchEffect(() => {
  if (!props.open) return;
  if (!selectedAgent.value) {
    selectedAgent.value = agentOptions.value[0]?.value ?? '';
  }
});

async function onSubmit() {
  submitError.value = null;
  submitting.value = true;
  try {
    const agentName = selectedAgent.value;
    if (!agentName) throw new Error('Select an agent');
    const room = await roomsStore.createFreeformRoom([`user:${userName.value || 'local'}`, `agent:${agentName}`]);
    emit('created', room.id);
    emit('close');
  } catch (e) {
    submitError.value = e instanceof Error ? e.message : String(e);
  } finally {
    submitting.value = false;
  }
}

function onCancel() {
  emit('close');
}
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="overlay" @click.self="onCancel">
      <div class="modal" role="dialog" aria-modal="true" aria-label="Create Room">
        <header class="header">
          <h2 class="h2">Create Room</h2>
          <button class="icon-btn" type="button" @click="onCancel" aria-label="Close">✕</button>
        </header>

        <form class="body" @submit.prevent="onSubmit">
          <label class="field">
            <span class="label">Your name</span>
            <input v-model="userName" class="input" type="text" placeholder="local" />
          </label>

          <label class="field">
            <span class="label">Agent</span>
            <select v-model="selectedAgent" class="input" :disabled="agentsStore.loading">
              <option v-for="opt in agentOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
            </select>
          </label>

          <p v-if="submitError" class="error">{{ submitError }}</p>

          <footer class="footer">
            <button type="button" @click="onCancel" :disabled="submitting">Cancel</button>
            <button class="primary" type="submit" :disabled="submitting || !selectedAgent">
              {{ submitting ? 'Creating…' : 'Create' }}
            </button>
          </footer>
        </form>
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
  width: min(36rem, 100%);
  background: var(--bg-elevated);
  border: 1px solid var(--border-subtle);
  border-radius: 0.75rem;
  box-shadow: 0 18px 60px rgba(0, 0, 0, 0.15);
  overflow: hidden;
}

.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 1rem 1rem 0.75rem;
  border-bottom: 1px solid var(--border-subtle);
}

.icon-btn {
  padding: 0.25rem 0.5rem;
  border: 1px solid var(--border-default);
  border-radius: 0.5rem;
  background: var(--bg-primary);
}

.body {
  padding: 1rem;
  display: grid;
  gap: 0.75rem;
}

.field {
  display: grid;
  gap: 0.35rem;
}

.label {
  font-size: var(--body-xs-size);
  color: var(--fg-tertiary);
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
}

.input {
  padding: 0.6rem 0.75rem;
  border-radius: 0.5rem;
  border: 1px solid var(--border-default);
  background: var(--bg-primary);
  color: var(--fg-primary);
}

.error {
  margin: 0;
  color: var(--error-dark);
}

.footer {
  display: flex;
  justify-content: flex-end;
  gap: 0.5rem;
  padding-top: 0.25rem;
}

.primary {
  border-color: var(--accent-primary);
  background: var(--accent-primary-soft);
}
</style>
