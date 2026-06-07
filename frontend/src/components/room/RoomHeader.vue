<script setup lang="ts">
import { PhGear } from '@phosphor-icons/vue';

defineProps<{
  participantCount: number;
  title: string;
}>();

defineEmits<{
  (e: 'settings'): void;
}>();
</script>

<template>
  <header class="room-header">
    <div class="title-block">
      <h1 class="title">{{ title }}</h1>
      <p class="meta">
        <span class="text-tertiary">{{ participantCount }} participants</span>
      </p>
    </div>

    <div class="actions">
      <button
        class="icon-button"
        type="button"
        aria-label="Room settings"
        @click="$emit('settings')"
      >
        <PhGear :size="20" weight="light" />
      </button>
    </div>
  </header>
</template>

<style scoped>
.room-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 1rem 1.25rem;
  border-bottom: 1px solid var(--border-subtle);
  background: var(--bg-elevated);
}

.title-block {
  min-width: 0;
}

.title {
  font-size: var(--body-lg-size);
  font-weight: var(--weight-medium);
  color: var(--fg-secondary);
  margin: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.meta {
  margin: 0.25rem 0 0;
  font-size: var(--body-sm-size);
}

.dot {
  margin: 0 0.5rem;
  color: var(--fg-muted);
}

.actions {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
</style>
