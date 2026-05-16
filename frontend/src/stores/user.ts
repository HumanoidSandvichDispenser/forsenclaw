import { computed, ref } from 'vue';
import { defineStore } from 'pinia';

export const useUserStore = defineStore('user', () => {
  const user = ref<null | { id: string; name: string }>(null);
  const isLoggedIn = computed(() => user.value !== null);

  function setUser(nextUser: { id: string; name: string }) {
    user.value = nextUser;
  }

  function clearUser() {
    user.value = null;
  }

  return { user, isLoggedIn, setUser, clearUser };
});
