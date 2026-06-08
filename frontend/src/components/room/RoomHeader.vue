<script setup lang="ts">
import { computed, nextTick, ref } from 'vue';
import { PhPencilSimple } from '@phosphor-icons/vue';

import type { RoomResponse } from '@/client';
import { roomName } from '@/stores/rooms';

const props = defineProps<{
  room: RoomResponse;
}>();

const emit = defineEmits<{
  (e: 'rename', name: string): void;
}>();

const name = computed(() => roomName(props.room));

const editing = ref(false);
const draft = ref('');
const inputEl = ref<HTMLInputElement | null>(null);

async function startEditing() {
  draft.value = props.room.name;
  editing.value = true;
  await nextTick();
  inputEl.value?.focus();
  inputEl.value?.select();
}

function commit() {
  if (!editing.value) return;
  editing.value = false;
  const next = draft.value.trim();
  if (next !== props.room.name) emit('rename', next);
}

function cancel() {
  editing.value = false;
}
</script>

<template>
  <header class="room-header">
    <div class="title-block">
      <input
        v-if="editing"
        ref="inputEl"
        v-model="draft"
        class="title title-input"
        type="text"
        placeholder="Room name"
        @keydown.enter="commit"
        @keydown.esc="cancel"
        @blur="commit"
      />
      <button
        v-else
        class="title title-button"
        type="button"
        title="Rename room"
        @click="startEditing"
      >
        <span class="title-text">{{ name }}</span>
        <PhPencilSimple class="edit-hint" :size="16" weight="light" />
      </button>
      <p class="meta">
        <span class="text-tertiary">Clearance {{ room.clearance }}</span>
      </p>
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
  width: 100%;
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

.title-button {
  display: flex;
  align-items: center;
  gap: 0.375rem;
  max-width: 100%;
  padding: 0.125rem 0.375rem;
  margin-left: -0.375rem;
  border: none;
  border-radius: 0.375rem;
  background: transparent;
  color: inherit;
  font: inherit;
  cursor: text;
}

.title-button:hover {
  background: var(--bg-secondary);
}

.title-text {
  overflow: hidden;
  text-overflow: ellipsis;
}

.edit-hint {
  flex-shrink: 0;
  opacity: 0;
  color: var(--fg-muted);
}

.title-button:hover .edit-hint {
  opacity: 1;
}

.title-input {
  width: 100%;
  padding: 0.125rem 0.375rem;
  margin-left: -0.375rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.375rem;
  background: var(--bg-base);
  color: var(--fg-primary);
}

.meta {
  margin: 0.25rem 0 0;
  font-size: var(--body-sm-size);
}
</style>
