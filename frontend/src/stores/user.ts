import { computed, ref } from 'vue';
import { defineStore } from 'pinia';

import { getMe, type UserResponse } from '@/client';
import { useClientStore } from '@/stores/client';

export const useUserStore = defineStore('user', () => {
  const clientStore = useClientStore();
  const user = ref<null | { id: string; name: string }>(null);
  const isLoggedIn = computed(() => user.value !== null);

  const loading = ref(false);
  const error = ref<string | null>(null);

  function setUser(nextUser: { id: string; name: string }) {
    user.value = nextUser;
  }

  function clearUser() {
    user.value = null;
  }

  async function fetchMe() {
    loading.value = true;
    error.value = null;
    try {
      const me: UserResponse = await getMe({ client: clientStore.client });
      user.value = { id: me.id, name: me.name };
      return me;
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
      // Keep app usable even if auth isn't configured yet.
      return null;
    } finally {
      loading.value = false;
    }
  }

  return {
    user,
    isLoggedIn,
    loading,
    error,
    setUser,
    clearUser,
    fetchMe,
  };
});
