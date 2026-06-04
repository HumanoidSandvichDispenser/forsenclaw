import { ref, watch } from 'vue';

// useDock holds the inspection dock's open/active state, shared across the app
// and persisted so a chosen panel stays open between rooms and reloads. The
// dock shows at most one panel at a time; a null activeId means collapsed (only
// the icon rail is visible).
const STORAGE_KEY = 'dock.activePanel';

const activeId = ref<string | null>(localStorage.getItem(STORAGE_KEY) || null);

watch(activeId, (id) => {
  if (id) localStorage.setItem(STORAGE_KEY, id);
  else localStorage.removeItem(STORAGE_KEY);
});

export function useDock() {
  // toggle opens the panel, or closes it if it is already active.
  function toggle(id: string) {
    activeId.value = activeId.value === id ? null : id;
  }

  function close() {
    activeId.value = null;
  }

  return { activeId, toggle, close };
}
