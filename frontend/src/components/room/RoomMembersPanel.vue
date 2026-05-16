<script setup lang="ts">
import type { ActorResponse } from '@/client';

defineProps<{
  members: ActorResponse[];
  meActorId: string;
}>();
</script>

<template>
  <aside class="right">
    <h2 class="right-title">Members</h2>
    <div class="members">
      <div v-for="m in members" :key="m.id" class="member">
        <div class="member-name">
          <span>{{ m.name }}</span>
          <span v-if="m.id === meActorId" class="you">You</span>
        </div>
        <div class="member-meta">{{ m.type }}</div>
      </div>
    </div>

    <h2 class="right-title">Memory</h2>
    <div class="memory">
      <p class="muted">Not implemented yet.</p>
    </div>
  </aside>
</template>

<style scoped>
.right {
  border-left: 1px solid var(--border-subtle);
  background: var(--bg-elevated);
  padding: 1rem;
  overflow-y: auto;
  min-height: 0;
}

.right-title {
  margin: 0;
  color: var(--fg-tertiary);
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.members {
  margin-top: 0.75rem;
  display: grid;
  gap: 0.5rem;
  padding-bottom: 1rem;
  border-bottom: 1px solid var(--border-subtle);
}

.member {
  padding: 0.5rem 0.6rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.75rem;
  background: var(--bg-primary);
}

.member-name {
  display: flex;
  justify-content: space-between;
  gap: 0.75rem;
}

.you {
  color: var(--fg-tertiary);
  font-size: var(--body-xs-size);
}

.member-meta {
  color: var(--fg-tertiary);
  font-size: var(--body-xs-size);
  margin-top: 0.15rem;
}

.memory {
  margin-top: 0.75rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.75rem;
  background: var(--bg-primary);
  padding: 0.75rem;
}

.muted {
  color: var(--fg-tertiary);
  margin: 0;
}
</style>
