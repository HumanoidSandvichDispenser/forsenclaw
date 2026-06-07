<script setup lang="ts">
import { PhPaperPlaneTilt } from '@phosphor-icons/vue';

defineProps<{
  error: string | null;
  modelValue: string;
  sending: boolean;
}>();

defineEmits<{
  (e: 'submit'): void;
  (e: 'update:modelValue', value: string): void;
}>();
</script>

<template>
  <form class="composer" @submit.prevent="$emit('submit')">
    <input
      class="input"
      type="text"
      placeholder="Message…"
      :disabled="sending"
      :value="modelValue"
      @input="$emit('update:modelValue', ($event.target as HTMLInputElement).value)"
    />
    <button class="icon-button send" type="submit" :disabled="sending || !modelValue.trim()">
      <PhPaperPlaneTilt :size="18" weight="light" />
    </button>
    <p v-if="error" class="error composer-error">{{ error }}</p>
  </form>
</template>

<style scoped>
.composer {
  border-top: 1px solid var(--border-subtle);
  background: var(--bg-elevated);
  padding: 0.75rem 1.25rem;
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 0.75rem;
  align-items: center;
}

.input {
  padding: 0.7rem 0.9rem;
  border-radius: 0.6rem;
  border: 1px solid var(--border-default);
  background: var(--bg-primary);
}

.send {
  --icon-button-size: 2.4rem;
}

.composer-error {
  grid-column: 1 / -1;
  margin: 0;
}

.error {
  color: var(--error-dark);
  margin: 0;
}
</style>
