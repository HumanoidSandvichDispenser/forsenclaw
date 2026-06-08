<script setup lang="ts">
import { computed, type Component } from 'vue';
import { PhX } from '@phosphor-icons/vue';
import { useDock } from '@/composables/useDock';

// A DockPanel registers an inspection surface with the dock. Adding a panel is
// declaring one of these in the host's `panels` list — no layout plumbing.
export interface DockPanel {
  id: string;
  title: string;
  icon: Component;
  component: Component;
  props?: Record<string, unknown>;
  // live shows an activity indicator on the rail icon (e.g. a DAG with nodes
  // in flight). Panels recompute this however they like and pass it in.
  live?: boolean;
}

const props = defineProps<{ panels: DockPanel[] }>();

const { activeId, toggle } = useDock();

// The active panel, or null when collapsed / when a stale persisted id no longer
// matches any registered panel.
const activePanel = computed(() => props.panels.find((p) => p.id === activeId.value) ?? null);
</script>

<template>
  <aside class="dock" :class="{ open: !!activePanel }">
    <section v-if="activePanel" class="dock-panel" aria-live="polite">
      <header class="dock-panel-header">
        <span class="dock-panel-title">{{ activePanel.title }}</span>
        <button
          class="icon-button dock-close"
          type="button"
          aria-label="Close panel"
          @click="toggle(activePanel.id)"
        >
          <PhX :size="16" weight="light" />
        </button>
      </header>
      <div class="dock-panel-body">
        <component :is="activePanel.component" v-bind="activePanel.props" />
      </div>
    </section>

    <nav class="dock-rail" aria-label="Inspection panels">
      <button
        v-for="p in panels"
        :key="p.id"
        class="icon-button rail-btn"
        :class="{ active: p.id === activeId }"
        type="button"
        :title="p.title"
        :aria-label="p.title"
        :aria-pressed="p.id === activeId"
        @click="toggle(p.id)"
      >
        <component
          :is="p.icon"
          class="rail-icon"
          :size="20"
          :weight="p.id === activeId ? 'regular' : 'light'"
        />
        <span v-if="p.live" class="rail-live" aria-hidden="true" />
      </button>
    </nav>
  </aside>
</template>

<style scoped>
.dock {
  display: flex;
  min-height: 0;
  border-left: 1px solid var(--border-subtle);
  background: var(--bg-elevated);
}

.dock-panel {
  display: flex;
  flex-direction: column;
  width: 22rem;
  min-width: 0;
  border-right: 1px solid var(--border-subtle);
}

/* On mobile a 22rem side column would not fit; the open panel takes over the
   screen instead, dismissed via its existing close button. The icon rail stays
   in flow as the entry point. */
@media (max-width: 768px) {
  .dock-panel {
    position: fixed;
    inset: 0;
    width: 100%;
    z-index: 45;
    border-right: none;
    background: var(--bg-elevated);
  }
}

.dock-panel-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0.6rem 0.9rem;
  border-bottom: 1px solid var(--border-subtle);
}

.dock-panel-title {
  font-size: var(--body-sm-size);
  font-weight: var(--weight-medium);
  color: var(--fg-secondary);
}

.dock-close {
  --icon-button-size: 1.5rem;
  border: 0;
  background: transparent;
  color: var(--fg-muted);
}

.dock-close:hover {
  color: var(--fg-primary);
}

.dock-panel-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 0.9rem;
}

.dock-rail {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  padding: 0.5rem 0.35rem;
}

.rail-btn {
  position: relative;
  border: 1px solid transparent;
  border-radius: 0.5rem;
  background: transparent;
  color: var(--fg-tertiary);
}

.rail-btn:hover {
  background: var(--bg-tertiary);
  color: var(--fg-secondary);
}

.rail-btn.active {
  background: var(--bg-secondary);
  border-color: var(--border-subtle);
  color: var(--fg-primary);
}

.rail-icon {
  display: block;
}

.rail-live {
  position: absolute;
  top: 0.3rem;
  right: 0.3rem;
  width: 0.45rem;
  height: 0.45rem;
  border-radius: 50%;
  background: var(--accent-primary);
}
</style>
