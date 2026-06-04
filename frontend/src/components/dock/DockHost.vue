<script setup lang="ts">
import { computed, type Component } from 'vue';
import { useDock } from '@/composables/useDock';

// A DockPanel registers an inspection surface with the dock. Adding a panel is
// declaring one of these in the host's `panels` list — no layout plumbing.
export interface DockPanel {
  id: string;
  title: string;
  icon: string;
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
          class="dock-close"
          type="button"
          aria-label="Close panel"
          @click="toggle(activePanel.id)"
        >×</button>
      </header>
      <div class="dock-panel-body">
        <component :is="activePanel.component" v-bind="activePanel.props" />
      </div>
    </section>

    <nav class="dock-rail" aria-label="Inspection panels">
      <button
        v-for="p in panels"
        :key="p.id"
        class="rail-btn"
        :class="{ active: p.id === activeId }"
        type="button"
        :title="p.title"
        :aria-label="p.title"
        :aria-pressed="p.id === activeId"
        @click="toggle(p.id)"
      >
        <span class="rail-icon">{{ p.icon }}</span>
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
  width: 1.5rem;
  height: 1.5rem;
  display: grid;
  place-items: center;
  border: 0;
  background: transparent;
  color: var(--fg-muted);
  font-size: var(--body-lg-size);
  line-height: 1;
  cursor: pointer;
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
  width: 2.25rem;
  height: 2.25rem;
  display: grid;
  place-items: center;
  border: 1px solid transparent;
  border-radius: 0.5rem;
  background: transparent;
  color: var(--fg-tertiary);
  cursor: pointer;
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
  font-size: var(--body-size);
  line-height: 1;
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
