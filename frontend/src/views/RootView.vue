<script setup lang="ts">
import { watch } from 'vue';
import { useRoute } from 'vue-router';
import { PhList } from '@phosphor-icons/vue';

import Sidebar from '@/components/Sidebar.vue';
import { useSidebar } from '@/composables/useSidebar';

const route = useRoute();
// Destructure so `open` is a top-level ref binding (Vue auto-unwraps it in the
// template); `sidebar.open` would be a nested ref and always truthy.
const { open, toggle, close } = useSidebar();

// Close the drawer whenever navigation happens, so tapping a room on mobile
// reveals the content rather than leaving the drawer covering it.
watch(
  () => route.fullPath,
  () => close(),
);
</script>

<template>
  <main class="root-layout" :class="{ 'drawer-open': open }">
    <header class="mobile-bar">
      <button
        class="icon-button menu-btn"
        type="button"
        aria-label="Open navigation"
        @click="toggle()"
      >
        <PhList :size="22" weight="light" />
      </button>
      <span class="brand">forsenClaw</span>
    </header>

    <div
      class="scrim"
      aria-hidden="true"
      @click="close()"
    />

    <Sidebar />

    <section class="root-content">
      <RouterView />
    </section>
  </main>
</template>

<style scoped>
.root-layout {
  display: flex;
  min-height: 100vh;
}

.root-content {
  flex: 1;
  min-width: 0;
}

/* The mobile top bar and drawer scrim are desktop-hidden; the media query below
   turns them on and converts the sidebar into an off-canvas drawer. */
.mobile-bar {
  display: none;
}

.scrim {
  display: none;
}

@media (max-width: 768px) {
  .root-layout {
    flex-direction: column;
    /* Bound the shell to the dynamic viewport so the top bar + content fit
       without the page scrolling; inner regions own their own scrolling. */
    height: 100dvh;
    overflow: hidden;
  }

  .mobile-bar {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.5rem 0.75rem;
    border-bottom: 1px solid var(--border-subtle);
    background: var(--bg-elevated);
  }

  .menu-btn {
    border: 1px solid var(--border-default);
    border-radius: 0.5rem;
    background: var(--bg-primary);
  }

  .brand {
    font-size: var(--body-lg-size);
    font-weight: var(--weight-medium);
    color: var(--fg-secondary);
  }

  /* Scrim sits above content but below the drawer, dimming and dismissing. */
  .drawer-open .scrim {
    display: block;
    position: fixed;
    inset: 0;
    z-index: 40;
    background: var(--overlay-scrim);
  }

  .root-content {
    flex: 1;
    min-height: 0;
  }
}
</style>
