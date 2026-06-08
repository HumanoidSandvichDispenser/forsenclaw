<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { PhX } from '@phosphor-icons/vue';

const props = defineProps<{
  open: boolean;
  agentName?: string;
  // Configured automatic target in KB, shown as the target placeholder.
  autoTargetKb?: string;
  compacting?: boolean;
}>();

const emit = defineEmits<{
  (e: 'close'): void;
  (e: 'submit', payload: { targetBytes?: number; instructions?: string }): void;
}>();

const targetKb = ref('');
const instructions = ref('');

// Reset the fields each time the modal opens.
watch(
  () => props.open,
  (open) => {
    if (open) {
      targetKb.value = '';
      instructions.value = '';
    }
  },
);

const parsedTarget = computed(() => {
  const kb = Number(targetKb.value);
  return Number.isFinite(kb) && kb > 0 ? kb : 0;
});

function onSubmit() {
  emit('submit', {
    targetBytes: parsedTarget.value > 0 ? parsedTarget.value * 1024 : undefined,
    instructions: instructions.value.trim() || undefined,
  });
}
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="overlay" @click.self="$emit('close')">
      <div class="modal" role="dialog" aria-modal="true" aria-label="Compact transcript">
        <header class="header">
          <h2 class="h2">Compact transcript</h2>
          <button class="icon-button icon-btn" type="button" @click="$emit('close')" aria-label="Close">
            <PhX :size="18" weight="light" />
          </button>
        </header>

        <form class="body" @submit.prevent="onSubmit">
          <p v-if="agentName" class="hint">
            Summarize <strong>{{ agentName }}</strong>'s oldest messages in this room into its
            daily notes and drop them from the live window.
          </p>

          <label class="field">
            <span class="label">Target size (KB)</span>
            <input
              v-model="targetKb"
              class="input"
              type="number"
              min="1"
              inputmode="numeric"
              :placeholder="autoTargetKb || 'configured default'"
            />
            <span class="sub-hint">Leave blank to use the configured default.</span>
          </label>

          <label class="field">
            <span class="label">Additional instructions</span>
            <textarea
              v-model="instructions"
              class="input textarea"
              rows="3"
              placeholder="e.g. emphasize decisions about the schema migration"
            />
          </label>

          <footer class="footer">
            <button type="button" @click="$emit('close')" :disabled="compacting">Cancel</button>
            <button class="primary" type="submit" :disabled="compacting">
              {{ compacting ? 'Compacting…' : 'Compact' }}
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
  width: min(32rem, 100%);
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
  --icon-button-size: 2rem;
  border: 1px solid var(--border-default);
  border-radius: 0.5rem;
  background: var(--bg-primary);
}

.body {
  padding: 1rem;
  display: grid;
  gap: 0.75rem;
}

.hint {
  margin: 0;
  font-size: var(--body-sm-size);
  color: var(--fg-tertiary);
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

.sub-hint {
  font-size: var(--body-xs-size);
  color: var(--fg-muted);
}

.input {
  padding: 0.6rem 0.75rem;
  border-radius: 0.5rem;
  border: 1px solid var(--border-default);
  background: var(--bg-primary);
  color: var(--fg-primary);
}

.textarea {
  resize: vertical;
  font: inherit;
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
