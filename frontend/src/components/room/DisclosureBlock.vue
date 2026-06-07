<script setup lang="ts">
import { ref, watch } from 'vue';
import { PhCaretRight } from '@phosphor-icons/vue';

const props = defineProps<{
  title: string;
  initialOpen?: boolean;
  // When provided, animates open/closed in response to external state transitions.
  // After the transition fires once, user clicks control state independently.
  open?: boolean;
}>();

const isOpen = ref(props.initialOpen ?? false);

watch(
  () => props.open,
  (val, prev) => {
    if (val === undefined || val === prev) return;
    if (val !== isOpen.value) isOpen.value = val;
  },
);

function onEnter(el: Element) {
  const e = el as HTMLElement;
  e.style.height = '0';
  e.style.overflow = 'hidden';
  requestAnimationFrame(() => {
    e.style.height = `${e.scrollHeight}px`;
  });
}

function onAfterEnter(el: Element) {
  const e = el as HTMLElement;
  e.style.height = 'auto';
  e.style.overflow = '';
}

function onLeave(el: Element) {
  const e = el as HTMLElement;
  e.style.height = `${e.scrollHeight}px`;
  e.style.overflow = 'hidden';
  requestAnimationFrame(() => {
    e.style.height = '0';
  });
}

function onAfterLeave(el: Element) {
  const e = el as HTMLElement;
  e.style.height = '';
  e.style.overflow = '';
}
</script>

<template>
  <div class="disclosure" :class="{ open: isOpen }">
    <button class="disclosure-summary" type="button" @click="isOpen = !isOpen">
      <PhCaretRight class="disclosure-caret" :size="12" weight="bold" />
      <slot name="title">{{ props.title }}</slot>
    </button>
    <Transition
      :css="false"
      @enter="onEnter"
      @after-enter="onAfterEnter"
      @leave="onLeave"
      @after-leave="onAfterLeave"
    >
      <div v-if="isOpen" class="disclosure-body">
        <slot />
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.disclosure {
  color: var(--fg-muted);
  font-family: var(--body-family);
}

.disclosure-summary {
  display: flex;
  align-items: center;
  gap: 0.35em;
  background: none;
  border: none;
  padding: 0;
  cursor: pointer;
  color: var(--fg-muted);
  font-size: var(--body-xs-size);
  font-family: inherit;
  text-align: left;
  width: 100%;
}

.disclosure-caret {
  flex-shrink: 0;
  transition: transform 0.2s ease;
}

.disclosure.open .disclosure-caret {
  transform: rotate(90deg);
}

.disclosure-body {
  transition: height 0.25s ease;
}
</style>
