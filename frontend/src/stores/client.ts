import { computed, ref } from 'vue';
import { defineStore } from 'pinia';

import { createClient, createConfig, type Client, type Config } from '@/client/client';

export const useClientStore = defineStore('client', () => {
  const baseUrl = computed(
    () => import.meta.env.VITE_API_BASE_URL?.trim() || 'http://localhost:8000',
  );

  const config = computed<Config>(() =>
    createConfig({
      baseUrl: baseUrl.value,
      // Keeps cookies/session working if backend uses them.
      credentials: 'include',
      // Make SDK calls return parsed JSON directly.
      responseStyle: 'data',
      // Prefer try/catch in stores over error unions.
      throwOnError: true,
    }),
  );

  // Recreate client when baseUrl changes.
  const client = ref<Client>(createClient(config.value));

  function refreshClient() {
    client.value = createClient(config.value);
  }

  return {
    baseUrl,
    client,
    refreshClient,
  };
});
