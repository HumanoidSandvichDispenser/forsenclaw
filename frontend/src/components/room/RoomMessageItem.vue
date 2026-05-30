<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue';
import { renderMarkdown, parseContent } from '@/utils/markdown';
import type { ContentPart, TextPart, ThoughtPart, ToolUsePart } from '@/utils/markdown';

import type { ActorResponse, MessageResponse } from '@/client';
import type { ToolCallEntry } from '@/stores/messages';
import DisclosureBlock from '@/components/room/DisclosureBlock.vue';
import RoomMessageSourceModal from '@/components/room/RoomMessageSourceModal.vue';

const props = defineProps<{
  // Finalized message mode
  message?: MessageResponse;
  mine?: boolean;
  timestampLabel?: string;
  toolMessages?: MessageResponse[];
  // Streaming mode (mutually exclusive with message)
  streaming?: {
    content: string;
    sender: ActorResponse;
    toolCalls: ToolCallEntry[];
  };
}>();

const isStreaming = computed(() => props.streaming != null);
const sender = computed(() => props.streaming?.sender ?? props.message?.sender);
const isMine = computed(() => !isStreaming.value && (props.mine ?? false));
const isSystem = computed(() => sender.value?.type === 'system');

// ---------------------------------------------------------------------------
// Streaming animation
// ---------------------------------------------------------------------------
const displayedContent = ref('');
let animFrame: number | null = null;
let lastTimestamp = 0;
const CHARS_PER_FRAME = 3;

function animate(timestamp: number) {
  const target = props.streaming?.content ?? '';
  const currentLen = displayedContent.value.length;
  const targetLen = target.length;

  if (currentLen >= targetLen) {
    animFrame = null;
    lastTimestamp = 0;
    return;
  }

  if (lastTimestamp === 0) lastTimestamp = timestamp;
  const elapsed = timestamp - lastTimestamp;
  const charsToReveal = Math.max(1, Math.floor((elapsed / 16) * CHARS_PER_FRAME));
  displayedContent.value = target.slice(0, Math.min(currentLen + charsToReveal, targetLen));
  lastTimestamp = timestamp;
  animFrame = requestAnimationFrame(animate);
}

function startAnimation() {
  if (animFrame) return;
  lastTimestamp = 0;
  animFrame = requestAnimationFrame(animate);
}

watch(() => props.streaming?.content, () => {
  if (isStreaming.value) startAnimation();
});

onUnmounted(() => {
  if (animFrame) cancelAnimationFrame(animFrame);
});

// ---------------------------------------------------------------------------
// Streaming mode: active/completed tool calls from ToolCallEntry[]
// ---------------------------------------------------------------------------
const completedToolCalls = computed(() => props.streaming?.toolCalls.filter((tc) => tc.done) ?? []);
const activeToolCall = computed(() => props.streaming?.toolCalls.find((tc) => !tc.done) ?? null);

// ---------------------------------------------------------------------------
// Finalized mode: tool displays built from toolMessages
// ---------------------------------------------------------------------------
interface ToolDisplay {
  name: string;
  args: Record<string, unknown> | null;
  result: string | null;
}

function stripToolResponseXML(s: string): string {
  return s.replace(/<tool_response[^>]*>([\s\S]*?)<\/tool_response>/g, '$1').trim();
}

const toolDisplays = computed((): ToolDisplay[] => {
  if (isStreaming.value) return [];
  const msgs = props.toolMessages ?? [];
  if (msgs.length === 0) return [];

  const callMsgs = msgs.filter((m) => m.type === 'tool_call');
  const resultMsgs = msgs.filter((m) => m.type === 'tool_result');
  const displays: ToolDisplay[] = [];

  const nativeCalls = callMsgs.flatMap((m) => m.tool_calls ?? []);
  if (nativeCalls.length > 0) {
    for (const tc of nativeCalls) {
      let args: Record<string, unknown> | null = null;
      try { args = JSON.parse(tc.arguments ?? ''); } catch { /* ignore */ }
      const resultMsg = resultMsgs.find((r) => r.tool_call_id === tc.id);
      displays.push({ name: tc.tool_name, args, result: resultMsg ? stripToolResponseXML(resultMsg.content) : null });
    }
    return displays;
  }

  for (const r of resultMsgs) {
    displays.push({ name: r.tool_name ?? 'tool', args: null, result: stripToolResponseXML(r.content) });
  }
  return displays;
});

const displayContent = computed(() => {
  if (isStreaming.value) return displayedContent.value;
  const content = props.message?.content ?? '';
  if ((props.toolMessages?.length ?? 0) === 0) return content;
  return content.replace(/<tool_use>[\s\S]*?<\/tool_use>\n?/g, '').trimStart();
});

// ---------------------------------------------------------------------------
// Source modal (finalized mode only)
// ---------------------------------------------------------------------------
const sourceOpen = ref(false);
</script>

<template>
  <article
    class="msg"
    :class="{
      mine: isMine,
      other: !isMine,
      system: isSystem,
      streaming: isStreaming,
    }"
  >
    <div class="bubble">
      <!-- Streaming: active tool indicator -->
      <div v-if="isStreaming && activeToolCall" class="tool-indicator">
        <span class="tool-spinner" />
        <span class="tool-text">Using {{ activeToolCall.name }}...</span>
      </div>

      <!-- Streaming: completed tool calls (name only, no args/result yet) -->
      <DisclosureBlock
        v-for="(tc, i) in completedToolCalls"
        :key="`stc-${i}`"
        :title="`Used ${tc.name}`"
        class="tool-use"
      />

      <!-- Finalized: tool displays from toolMessages -->
      <DisclosureBlock
        v-for="(td, i) in toolDisplays"
        :key="`td-${i}`"
        :title="`Used ${td.name}`"
        class="tool-use"
      >
        <div v-if="td.args" class="tool-body">
          <pre class="tool-args">{{ JSON.stringify(td.args, null, 2) }}</pre>
        </div>
        <div v-if="td.result" class="tool-body tool-result">{{ td.result }}</div>
      </DisclosureBlock>

      <!-- Content parts -->
      <template v-for="(part, idx) in parseContent(displayContent, isStreaming)" :key="idx">
        <DisclosureBlock
          v-if="part.type === 'thought'"
          :title="(part as ThoughtPart).title"
          :initial-open="isStreaming"
          :open="isStreaming"
          class="thought"
        >
          <div class="content" v-html="renderMarkdown((part as ThoughtPart).content)" />
        </DisclosureBlock>
        <DisclosureBlock
          v-else-if="part.type === 'tool_use'"
          :title="`Used ${(part as ToolUsePart).name}`"
          class="tool-use"
        >
          <div v-if="(part as ToolUsePart).args" class="tool-body">
            <pre class="tool-args">{{ JSON.stringify((part as ToolUsePart).args, null, 2) }}</pre>
          </div>
          <div v-if="(part as ToolUsePart).result" class="tool-body tool-result">
            {{ (part as ToolUsePart).result }}
          </div>
        </DisclosureBlock>
        <div v-else-if="!isMine" class="content" v-html="renderMarkdown((part as TextPart).content)" />
        <p v-else class="content">{{ (part as TextPart).content }}</p>
      </template>

      <!-- Streaming cursor -->
      <span v-if="isStreaming" class="cursor" />

      <!-- Speaker row -->
      <div class="speaker">
        <span class="name">{{ sender?.name }}</span>
        <div class="side">
          <template v-if="isStreaming">
            <span class="typing-label">typing...</span>
          </template>
          <template v-else>
            <button class="time-btn" type="button" @click="sourceOpen = true">View Source</button>
            <span class="time">{{ props.timestampLabel }}</span>
          </template>
        </div>
      </div>
    </div>
    <RoomMessageSourceModal
      v-if="!isStreaming && props.message"
      :open="sourceOpen"
      :message="props.message"
      @close="sourceOpen = false"
    />
  </article>
</template>

<style scoped>
.msg {
  display: flex;
  font-size: var(--body-sm-size);
}

.msg.mine {
  justify-content: flex-end;
}

.bubble {
  width: 100%;
  border-radius: 0.9rem;
  padding: 0.75rem 0.9rem;
}

.bubble p {
  font-size: var(--body-size);
}

.msg.system .bubble {
  border: 1px solid var(--border-subtle);
}

.msg.other:hover .bubble {
  background: var(--bg-secondary);
}

.msg.mine .bubble {
  border: 1px solid var(--border-subtle);
  background: var(--bg-secondary);
}

.msg.other .content {
  font-family: var(--font-body-serif);
  font-size: var(--body-size);
}

.msg.other.system .content {
  font-family: var(--code-family);
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

p.content {
  margin: 0;
  white-space: pre-wrap;
}

:deep(.content) {
  margin: 0;
}

:deep(.content p) {
  margin: 0 0 0.5rem;
}

:deep(.content p:last-child) {
  margin-bottom: 0;
}

:deep(.content code) {
  padding: 0.15em 0.4em;
  border-radius: 0.3em;
  font-family: var(--code-family);
  font-size: 0.9em;
}

:deep(.content pre) {
  border: 1px solid var(--border-subtle);
  padding: 1rem;
  border-radius: 0.5rem;
  overflow-x: auto;
  margin: 0.5rem 0;
}

:deep(.content pre code) {
  background: none;
  padding: 0;
  border-radius: 0;
}

:deep(.content ul),
:deep(.content ol) {
  margin: 0.5rem 0;
  padding-left: 1.5rem;
}

:deep(.content li) {
  margin: 0.25rem 0;
}

:deep(.content a) {
  color: var(--accent);
  text-decoration: none;
}

:deep(.content a:hover) {
  text-decoration: underline;
}

:deep(.content h1),
:deep(.content h2),
:deep(.content h3),
:deep(.content h4),
:deep(.content h5),
:deep(.content h6) {
  margin: 1rem 0 0.5rem;
  font-weight: 600;
}

:deep(.content h1:first-child),
:deep(.content h2:first-child),
:deep(.content h3:first-child),
:deep(.content h4:first-child),
:deep(.content h5:first-child),
:deep(.content h6:first-child) {
  margin-top: 0;
}

:deep(.content blockquote) {
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

.msg .thought,
.msg .tool-use {
  margin-bottom: 0.25rem;
}

.msg .thought :deep(.disclosure-body p) {
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

.time {
  color: var(--fg-muted);
}

.time-btn {
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--fg-muted);
  font: inherit;
}

.time-btn:hover {
  color: var(--fg-primary);
}
</style>
