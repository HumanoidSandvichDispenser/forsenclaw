<script setup lang="ts">
defineProps<{
  open: boolean;
  label: string;
  // Optional max content width (e.g. "52rem"). Defaults to 48rem.
  width?: string;
}>();

const emit = defineEmits<{
  (e: 'close'): void;
}>();
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="overlay" @click.self="emit('close')">
      <div
        class="modal"
        :style="width ? { '--modal-width': width } : undefined"
        role="dialog"
        aria-modal="true"
        :aria-label="label"
      >
        <header class="header">
          <div class="header-main"><slot name="header" /></div>
          <button class="icon-btn" type="button" aria-label="Close" @click="emit('close')">✕</button>
        </header>
        <div class="content"><slot /></div>
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
  width: min(var(--modal-width, 48rem), 100%);
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

.header-main {
  min-width: 0;
}

.icon-btn {
  padding: 0.25rem 0.5rem;
  border: 1px solid var(--border-default);
  border-radius: 0.5rem;
  background: var(--bg-primary);
}

.content {
  overflow: auto;
  min-height: 0;
}
</style>
