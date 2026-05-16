<script setup lang="ts">
import Sidebar from '@/components/Sidebar.vue';
</script>

<template>
  <main class="root-layout">
    <Sidebar />
    <section class="root-content">
      <RouterView />
    </section>
  </main>
</template>

<style scoped>
.root-layout {
  display: flex;
  min-height: 100vh;
}

.root-content {
  flex: 1;
  min-width: 0;
}
</style>
