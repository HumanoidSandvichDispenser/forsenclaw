<script setup lang="ts">
import { computed, onMounted } from 'vue';

import { useRoomsStore } from '@/stores/rooms';

const roomsStore = useRoomsStore();

onMounted(() => {
  if (roomsStore.rooms.length === 0) roomsStore.fetchRooms();
});

const rooms = computed(() => roomsStore.rooms);
</script>

<template>
  <section class="rooms-view">
    <header class="header">
      <h1 class="h2">Rooms</h1>
      <p class="text-tertiary hint">Select a room from the sidebar.</p>
    </header>

    <p v-if="roomsStore.loading" class="text-tertiary">Loading…</p>
    <p v-else-if="roomsStore.error" class="error">{{ roomsStore.error }}</p>

    <div v-else class="list">
      <RouterLink
        v-for="r in rooms"
        :key="r.id"
        class="row"
        :to="`/rooms/${r.id}`"
      >
        <div class="title">{{ (r.participants ?? []).map((p) => p.name).join(' · ') || r.id }}</div>
      </RouterLink>
    </div>
  </section>
</template>

<style scoped>
.rooms-view {
  padding: 1.25rem;
}

.header {
  padding-bottom: 1rem;
  border-bottom: 1px solid var(--border-subtle);
  margin-bottom: 1rem;
}

.hint {
  margin: 0.25rem 0 0;
}

.error {
  color: var(--error-dark);
}

.list {
  display: grid;
  gap: 0.5rem;
}

.row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.75rem 0.9rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.75rem;
  background: var(--bg-elevated);
  text-decoration: none;
}

.row:hover {
  background: var(--bg-secondary);
}

.title {
  color: var(--fg-primary);
}

.meta {
  color: var(--fg-tertiary);
  font-size: var(--body-xs-size);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  font-weight: 700;
}
</style>
