<script setup lang="ts">
import { RouterLink } from 'vue-router';

defineProps<{
  to: string;
}>();
</script>

<template>
  <RouterLink class="sidebar-tab" :to="to">
    <slot></slot>
  </RouterLink>
</template>

<style scoped>
.sidebar-tab {
  display: block;
  padding: 0.5rem 1rem;
  border-radius: 0.5rem;
  color: var(--fg-secondary);
  text-decoration: none;
}

.sidebar-tab:hover,
.sidebar-tab.router-link-active {
  background: var(--bg-secondary);
}

.sidebar-tab.router-link-active {
  color: var(--fg-primary);
}
</style>
