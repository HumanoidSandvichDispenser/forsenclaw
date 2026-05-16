<script setup lang="ts">
defineProps<{
  label: string;
}>();
</script>

<template>
  <section class="sidebar-list">
    <h2 class="label">{{ label }}</h2>
    <div class="items">
      <slot />
    </div>
  </section>
</template>

<style scoped>
.sidebar-list .label {
  margin: 0 1rem 0.5rem;
  color: var(--fg-tertiary);
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.sidebar-list .items {
  display: grid;
  gap: 0.25rem;
}
</style>
