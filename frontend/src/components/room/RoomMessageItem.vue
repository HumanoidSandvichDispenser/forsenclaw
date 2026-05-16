<script setup lang="ts">
import type { MessageResponse } from '@/client';

defineProps<{
  message: MessageResponse;
  mine: boolean;
  timestampLabel: string;
}>();
</script>

<template>
  <article class="msg" :class="{ mine }">
    <div class="bubble">
      <p class="body">{{ message.content }}</p>
      <p class="speaker">
        <span class="name">{{ message.sender.name }}</span>
        <span class="time">{{ timestampLabel }}</span>
      </p>
    </div>
  </article>
</template>

<style scoped>
.msg {
  display: flex;
}

.msg.mine {
  justify-content: flex-end;
}

.bubble {
  width: 100%;
  padding: 0.75rem 0.9rem;
}

.msg.mine .bubble {
  border: 1px solid var(--border-subtle);
  border-radius: 0.9rem;
  background: var(--bg-elevated);
  background: var(--bg-secondary);
}

.body {
  margin: 0;
  white-space: pre-wrap;
}

.speaker {
  margin: 0.5rem 0 0;
  display: flex;
  gap: 0.5rem;
  align-items: baseline;
  justify-content: space-between;
  color: var(--fg-tertiary);
  font-size: var(--body-xs-size);
}

.name {
  font-weight: 600;
}

.time {
  color: var(--fg-muted);
}
</style>
