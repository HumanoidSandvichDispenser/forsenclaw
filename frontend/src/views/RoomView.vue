<script setup lang="ts">
import { useRoute } from 'vue-router';

const route = useRoute();
</script>

<template>
  <section>
    <h1>{{ route.params.roomId }}</h1>
  </section>
</template>
