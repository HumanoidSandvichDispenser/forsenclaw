import { computed } from 'vue';
import { defineStore } from 'pinia';

import { createClient, createConfig, type Client, type Config } from '@/client/client';

export const useClientStore = defineStore('client', () => {
  const baseUrl = computed(
    () => import.meta.env.VITE_API_BASE_URL?.trim() || window.location.origin,
  );

  const config = computed<Config>(() =>
    createConfig({
      baseUrl: baseUrl.value,
      // Keeps cookies/session working if backend uses them.
      credentials: 'include',
      // Prefer try/catch in stores over error unions.
      throwOnError: true,
    }),
  );

  const client = computed<Client>(() => createClient(config.value));

  return {
    baseUrl,
    client,
  };
});
