<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue';

import type { ActorResponse } from '@/client';

const props = defineProps<{
  content: string;
  sender: ActorResponse;
  toolCall: string | null;
}>();

const displayedContent = ref('');
let animFrame: number | null = null;
let lastTimestamp = 0;
const CHARS_PER_FRAME = 3;

function animate(timestamp: number) {
  const target = props.content;
  const currentLen = displayedContent.value.length;
  const targetLen = target.length;

  if (currentLen >= targetLen) {
    animFrame = null;
    lastTimestamp = 0;
    return;
  }

  if (lastTimestamp === 0) {
    lastTimestamp = timestamp;
  }

  const elapsed = timestamp - lastTimestamp;
  const charsToReveal = Math.max(1, Math.floor((elapsed / 16) * CHARS_PER_FRAME));
  const newLen = Math.min(currentLen + charsToReveal, targetLen);

  displayedContent.value = target.slice(0, newLen);
  lastTimestamp = timestamp;

  animFrame = requestAnimationFrame(animate);
}

function startAnimation() {
  if (animFrame) return;
  lastTimestamp = 0;
  animFrame = requestAnimationFrame(animate);
}

watch(() => props.content, () => {
  startAnimation();
});

onUnmounted(() => {
  if (animFrame) cancelAnimationFrame(animFrame);
});

function parseContent(content: string) {
  const parts: Array<{ type: string; content: string; title?: string }> = [];
  const regex = /<thought>([\s\S]*?)<\/thought>/g;
  let lastIndex = 0;
  let match;

  while ((match = regex.exec(content)) !== null) {
    if (match.index > lastIndex) {
      parts.push({ type: 'text', content: content.slice(lastIndex, match.index) });
    }
    parts.push({ type: 'thought', title: 'Thought', content: match[1] ?? '' });
    lastIndex = match.index + match[0].length;
  }

  const remainder = content.slice(lastIndex);
  const openThoughtIdx = remainder.lastIndexOf('<thought>');
  if (openThoughtIdx !== -1) {
    if (openThoughtIdx > 0) {
      parts.push({ type: 'text', content: remainder.slice(0, openThoughtIdx) });
    }
    parts.push({ type: 'thought', title: 'Thinking...', content: remainder.slice(openThoughtIdx + '<thought>'.length) });
  } else if (remainder.length > 0) {
    parts.push({ type: 'text', content: remainder });
  }

  return parts;
}

const parsedParts = computed(() => parseContent(displayedContent.value));
</script>

<template>
  <article class="msg other">
    <div class="bubble">
      <div v-if="props.toolCall" class="tool-indicator">
        <span class="tool-spinner" />
        <span class="tool-text">Using {{ props.toolCall }}...</span>
      </div>
      <template v-for="(part, idx) in parsedParts" :key="idx">
        <details v-if="part.type === 'thought'" class="thought" open>
          <summary>{{ part.title }}</summary>
          <p>{{ part.content }}</p>
        </details>
        <p v-else class="content">{{ part.content }}</p>
      </template>
      <span class="cursor" />
      <div class="speaker">
        <span class="name">{{ props.sender.name }}</span>
        <div class="side">
          <span class="typing-label">typing...</span>
        </div>
      </div>
    </div>
  </article>
</template>

<style scoped>
.msg {
  display: flex;
  font-size: var(--body-sm-size);
}

.msg.other:hover .bubble {
  background: var(--bg-secondary);
}

.bubble {
  width: 100%;
  border-radius: 0.9rem;
  padding: 0.75rem 0.9rem;
}

.bubble p {
  font-size: var(--body-size);
}

.tool-indicator {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.4rem 0.6rem;
  margin-bottom: 0.5rem;
  background: var(--bg-tertiary);
  border-radius: 0.5rem;
  font-size: var(--body-xs-size);
  color: var(--fg-secondary);
}

.tool-spinner {
  display: inline-block;
  width: 0.75rem;
  height: 0.75rem;
  border: 2px solid var(--border-subtle);
  border-top-color: var(--fg-secondary);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.msg.other p.content {
  font-family: var(--font-body-serif);
}

p.content {
  margin: 0;
  white-space: pre-wrap;
}

.cursor {
  display: inline-block;
  width: 0.5em;
  height: 1.2em;
  background: var(--fg-primary);
  vertical-align: text-bottom;
  animation: blink 1s step-end infinite;
}

@keyframes blink {
  50% { opacity: 0; }
}

.speaker {
  margin: 0.5rem 0 0;
  display: flex;
  gap: 0.5rem;
  align-items: baseline;
  justify-content: space-between;
  color: var(--fg-tertiary);
  font-family: var(--body-family);
  font-size: var(--body-xs-size);
}

.name {
  font-weight: 600;
}

.side {
  display: flex;
  gap: 0.5rem;
  align-items: center;
  opacity: 0;
  transition: opacity 0.2s;
}

.msg:hover .side {
  opacity: 1;
}

.typing-label {
  color: var(--fg-muted);
  font-style: italic;
}

.msg details.thought {
  border-radius: 0.75rem;
  color: var(--fg-muted);
  font-family: var(--body-family);
  cursor: pointer;
}

.msg details.thought p {
  margin: 0.5rem 0 0;
  color: var(--fg-muted);
  font-size: var(--body-xs-size);
}

.msg details.thought summary {
  color: var(--fg-muted);
  font-size: var(--body-xs-size);
}
</style>
