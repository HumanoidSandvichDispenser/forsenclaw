<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';

import { getAgent, type AgentResponse } from '@/client';
import { useClientStore } from '@/stores/client';

const route = useRoute();
const clientStore = useClientStore();

const agent = ref<AgentResponse | null>(null);
const loading = ref(false);
const error = ref<string | null>(null);

onMounted(async () => {
  loading.value = true;
  error.value = null;
  try {
    const res = await getAgent({
      client: clientStore.client,
      path: { name: String(route.params.agentName) },
      throwOnError: true,
    });
    agent.value = (res as any)?.data ?? res;
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e);
  } finally {
    loading.value = false;
  }
});
</script>

<template>
  <section class="agent-view">
    <header class="header">
      <h1 class="h2">{{ String(route.params.agentName) }}</h1>
      <p class="sub text-tertiary">Agent details</p>
    </header>

    <p v-if="loading" class="text-tertiary">Loading…</p>
    <p v-else-if="error" class="error">{{ error }}</p>

    <div v-else-if="agent" class="card">
      <div class="row">
        <span class="k">Active</span>
        <span class="v">{{ agent.active ? 'yes' : 'no' }}</span>
      </div>
      <div class="row">
        <span class="k">Clearance</span>
        <span class="v">{{ agent.clearance }}</span>
      </div>
      <div class="row">
        <span class="k">Primary model</span>
        <span class="v mono">{{ agent.primary_model }}</span>
      </div>
      <div class="row">
        <span class="k">Role</span>
        <span class="v">{{ agent.role_description }}</span>
      </div>
    </div>
  </section>
</template>

<style scoped>
.agent-view {
  padding: 1.25rem;
}

.header {
  display: grid;
  gap: 0.25rem;
  padding-bottom: 1rem;
  border-bottom: 1px solid var(--border-subtle);
  margin-bottom: 1rem;
}

.sub {
  margin: 0;
}

.error {
  color: var(--error-dark);
}

.card {
  border: 1px solid var(--border-subtle);
  background: var(--bg-elevated);
  border-radius: 0.75rem;
  padding: 1rem;
  display: grid;
  gap: 0.5rem;
}

.row {
  display: flex;
  justify-content: space-between;
  gap: 1rem;
}

.k {
  color: var(--fg-tertiary);
  font-size: var(--body-xs-size);
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.08em;
}

.v {
  color: var(--fg-primary);
}

.mono {
  font-family: var(--font-mono);
  font-size: var(--body-sm-size);
}
</style>
