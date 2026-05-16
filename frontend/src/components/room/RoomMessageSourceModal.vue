<script setup lang="ts">
import type { MessageResponse } from '@/client';

const props = defineProps<{
  open: boolean;
  message: MessageResponse;
}>();

const emit = defineEmits<{
  (e: 'close'): void;
}>();

function onClose() {
  emit('close');
}
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="overlay" @click.self="onClose">
      <div class="modal" role="dialog" aria-modal="true" aria-label="Message source">
        <header class="header">
          <div>
            <h2 class="h2">Message Source</h2>
            <p class="meta">{{ props.message.sender.name }}</p>
          </div>
          <button class="icon-btn" type="button" @click="onClose" aria-label="Close">✕</button>
        </header>

        <pre class="source">{{ props.message.content }}</pre>
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
  width: min(48rem, 100%);
  max-height: min(80vh, 52rem);
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

.icon-btn {
  padding: 0.25rem 0.5rem;
  border: 1px solid var(--border-default);
  border-radius: 0.5rem;
  background: var(--bg-primary);
}

.source {
  margin: 0;
  padding: 1rem;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: var(--code-family);
  font-size: var(--body-sm-size);
  color: var(--fg-primary);
}
</style>
