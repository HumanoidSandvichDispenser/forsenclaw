<script setup lang="ts">
import type { MessageResponse } from '@/client';
import BaseModal from '@/components/BaseModal.vue';

const props = defineProps<{
  open: boolean;
  message: MessageResponse;
}>();

const emit = defineEmits<{
  (e: 'close'): void;
}>();
</script>

<template>
  <BaseModal :open="open" label="Message source" @close="emit('close')">
    <template #header>
      <h2 class="h2">Message Source</h2>
      <div class="meta">{{ props.message.sender.name }}</div>
      <div class="meta" v-if="props.message.usage">
        <span>{{ props.message.usage.input_tokens }} input tokens</span>
        &middot;
        <span>{{ props.message.usage.output_tokens }} output tokens</span>
      </div>
    </template>

    <pre class="source">{{ props.message.content }}</pre>
  </BaseModal>
</template>

<style scoped>
.h2 {
  margin: 0;
}

.meta {
  margin: 0.25rem 0 0;
  color: var(--fg-tertiary);
  font-size: var(--body-xs-size);
}

.source {
  margin: 0;
  padding: 1rem;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: var(--code-family);
  font-size: var(--body-sm-size);
  color: var(--fg-primary);
}
</style>
