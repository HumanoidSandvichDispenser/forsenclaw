<script setup lang="ts">
import { computed, onMounted } from 'vue';
import { useAgentsStore } from '@/stores/agents';

const props = defineProps<{ agentName: string }>();

const agentsStore = useAgentsStore();

onMounted(() => {
  if (agentsStore.agents.length === 0 && !agentsStore.loading) agentsStore.fetchAgents();
});

const agent = computed(() => agentsStore.byName.get(props.agentName) ?? null);
</script>

<template>
  <div class="agent-info">
    <p v-if="agentsStore.loading && !agent" class="muted">Loading…</p>
    <p v-else-if="!agent" class="muted">No info for {{ agentName || 'this agent' }}.</p>
    <template v-else>
      <div class="row">
        <span class="k">Name</span>
        <span class="v">{{ agent.name }}</span>
      </div>
      <div class="row">
        <span class="k">Status</span>
        <span class="v">{{ agent.active ? 'Loaded' : 'Inactive' }}</span>
      </div>
      <div class="row">
        <span class="k">Clearance</span>
        <span class="v">{{ agent.clearance }}</span>
      </div>
      <div class="row">
        <span class="k">Primary model</span>
        <span class="v">{{ agent.primary_model }}</span>
      </div>
      <div class="row">
        <span class="k">Routine model</span>
        <span class="v">{{ agent.routine_model }}</span>
      </div>
      <div class="row">
        <span class="k">Sensitive model</span>
        <span class="v">{{ agent.sensitive_model }}</span>
      </div>
      <div class="role">
        <span class="k">Role</span>
        <p class="role-text">{{ agent.role_description }}</p>
      </div>
    </template>
  </div>
</template>

<style scoped>
.agent-info {
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
  font-size: var(--body-sm-size);
}

.row {
  display: flex;
  justify-content: space-between;
  gap: 0.75rem;
}

.k {
  color: var(--fg-tertiary);
}

.v {
  color: var(--fg-secondary);
  text-align: right;
  word-break: break-word;
}

.role {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
  margin-top: 0.4rem;
  padding-top: 0.6rem;
  border-top: 1px solid var(--border-subtle);
}

.role-text {
  margin: 0;
  color: var(--fg-secondary);
  font-family: var(--font-body-serif);
  white-space: pre-wrap;
}

.muted {
  color: var(--fg-tertiary);
  margin: 0;
}
</style>
