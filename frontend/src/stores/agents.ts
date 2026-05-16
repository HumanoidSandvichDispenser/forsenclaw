import { computed, ref } from 'vue';
import { defineStore } from 'pinia';

import { listAgents, type AgentResponse } from '@/client';
import { useClientStore } from '@/stores/client';

export const useAgentsStore = defineStore('agents', () => {
  const clientStore = useClientStore();

  const agents = ref<AgentResponse[]>([]);
  const byName = computed(() => new Map(agents.value.map((a) => [a.name, a])));
  const loading = ref(false);
  const error = ref<string | null>(null);

  async function fetchAgents() {
    loading.value = true;
    error.value = null;
    try {
      const res = await listAgents({ client: clientStore.client });
      agents.value = res.agents ?? [];
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      loading.value = false;
    }
  }

  return {
    agents,
    byName,
    loading,
    error,
    fetchAgents,
  };
});
