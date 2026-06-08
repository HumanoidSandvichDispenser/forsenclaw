import { ref } from 'vue';

// useSidebar holds the navigation sidebar's open state, shared across the app.
// It only matters below the mobile breakpoint, where the sidebar is an
// off-canvas drawer; on desktop the sidebar is always visible and this state is
// ignored by the layout. Defaults closed so a fresh load on mobile shows content.
const open = ref(false);

export function useSidebar() {
  function toggle() {
    open.value = !open.value;
  }

  function close() {
    open.value = false;
  }

  return { open, toggle, close };
}
