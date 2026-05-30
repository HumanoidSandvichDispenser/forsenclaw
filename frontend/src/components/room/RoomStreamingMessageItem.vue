<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue';
import { renderMarkdown, parseContent } from '@/utils/markdown';
import type { TextPart, ThoughtPart, ToolUsePart } from '@/utils/markdown';

import type { ActorResponse } from '@/client';
import type { ToolCallEntry } from '@/stores/messages';

const props = defineProps<{
  content: string;
  sender: ActorResponse;
  toolCalls: ToolCallEntry[];
}>();

const completedToolCalls = computed(() => props.toolCalls.filter((tc) => tc.done));
const activeToolCall = computed(() => props.toolCalls.find((tc) => !tc.done) ?? null);

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

const parsedParts = computed(() => parseContent(displayedContent.value, true));
</script>

<template>
  <article class="msg other">
    <div class="bubble">
      <details
        v-for="(tc, i) in completedToolCalls"
        :key="i"
        class="tool-use"
      >
        <summary>Used {{ tc.name }}</summary>
      </details>
      <div v-if="activeToolCall" class="tool-indicator">
        <span class="tool-spinner" />
        <span class="tool-text">Using {{ activeToolCall.name }}...</span>
      </div>
      <template v-for="(part, idx) in parsedParts" :key="idx">
        <details v-if="part.type === 'thought'" class="thought" open>
          <summary>{{ (part as ThoughtPart).title }}</summary>
          <p>{{ (part as ThoughtPart).content }}</p>
        </details>
        <details v-else-if="part.type === 'tool_use'" class="tool-use">
          <summary>Used {{ (part as ToolUsePart).name }}</summary>
          <div v-if="(part as ToolUsePart).args" class="tool-body">
            <pre class="tool-args">{{ JSON.stringify((part as ToolUsePart).args, null, 2) }}</pre>
          </div>
          <div v-if="(part as ToolUsePart).result" class="tool-body tool-result">{{ (part as ToolUsePart).result }}</div>
        </details>
        <div v-else class="content" v-html="renderMarkdown((part as TextPart).content)" />
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

.msg.other .content {
  font-family: var(--font-body-serif);
}

.msg.other .content p {
  font-family: var(--font-body-serif);
}

.content {
  margin: 0;
}

.content p {
  margin: 0 0 0.5rem;
}

.content p:last-child {
  margin-bottom: 0;
}

.content code {
  padding: 0.15em 0.4em;
  border-radius: var(--border-radius);
  border: 1px solid var(--border-subtle);
  font-family: var(--code-family);
  font-size: 0.9em;
}

.content pre {
  padding: 1rem;
  border-radius: 0.5rem;
  overflow-x: auto;
  margin: 0.5rem 0;
}

.content pre code {
  background: none;
  padding: 0;
  border-radius: 0;
}

.content ul,
.content ol {
  margin: 0.5rem 0;
  padding-left: 1.5rem;
}

.content li {
  margin: 0.25rem 0;
}

.content a {
  color: var(--accent);
  text-decoration: none;
}

.content a:hover {
  text-decoration: underline;
}

.content h1,
.content h2,
.content h3,
.content h4,
.content h5,
.content h6 {
  margin: 1rem 0 0.5rem;
  font-weight: 600;
}

.content h1:first-child,
.content h2:first-child,
.content h3:first-child,
.content h4:first-child,
.content h5:first-child,
.content h6:first-child {
  margin-top: 0;
}

.content blockquote {
  border-left: 3px solid var(--border-subtle);
  padding-left: 1rem;
  margin: 0.5rem 0;
  color: var(--fg-muted);
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

.msg details.thought,
.msg details.tool-use {
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

.tool-body {
  margin: 0.4rem 0 0;
  font-size: var(--body-xs-size);
  color: var(--fg-muted);
}

.tool-args {
  margin: 0;
  padding: 0.4rem 0.6rem;
  border-radius: 0.4rem;
  border: 1px solid var(--border-subtle);
  font-family: var(--code-family);
  white-space: pre-wrap;
  overflow-x: auto;
}

.tool-result {
  padding: 0.4rem 0.6rem;
  border-radius: 0.4rem;
  border: 1px solid var(--border-subtle);
  white-space: pre-wrap;
  word-break: break-word;
}

.msg details.thought summary,
.msg details.tool-use summary {
  color: var(--fg-muted);
  font-size: var(--body-xs-size);
}
</style>
