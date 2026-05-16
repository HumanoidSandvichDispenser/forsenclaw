import { ref } from 'vue';
import { defineStore } from 'pinia';

export type Agent = {
  id: string;
  name: string;
};

export const useAgentsStore = defineStore('agents', () => {
  const agents = ref<Agent[]>([]);

  function setAgents(nextAgents: Agent[]) {
    agents.value = nextAgents;
  }

  return { agents, setAgents };
});
