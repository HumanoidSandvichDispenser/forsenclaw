<script setup lang="ts">
import { computed, ref } from 'vue';
import { marked } from 'marked';

import type { MessageResponse } from '@/client';
import RoomMessageSourceModal from '@/components/room/RoomMessageSourceModal.vue';

const props = defineProps<{
  message: MessageResponse;
  mine: boolean;
  timestampLabel: string;
  toolMessages?: MessageResponse[];
}>();

const sourceOpen = ref(false);

interface ToolUsePart {
  type: 'tool_use';
  name: string;
  args: Record<string, unknown> | null;
  result: string | null;
}
interface TextPart { type: 'text'; content: string }
interface ThoughtPart { type: 'thought'; title: string; content: string }
type ContentPart = TextPart | ThoughtPart | ToolUsePart;

function parseToolUse(raw: string): ToolUsePart {
  try {
    const data = JSON.parse(raw);
    return { type: 'tool_use', name: data.name ?? raw.trim(), args: data.args ?? null, result: data.result ?? null };
  } catch {
    return { type: 'tool_use', name: raw.trim(), args: null, result: null };
  }
}

function parseContent(content: string): ContentPart[] {
  const parts: ContentPart[] = [];
  const regex = /<thought>([\s\S]*?)<\/thought>|<tool_use>([\s\S]*?)<\/tool_use>/g;
  let lastIndex = 0;
  let match;

  while ((match = regex.exec(content)) !== null) {
    if (match.index > lastIndex) {
      parts.push({ type: 'text', content: content.slice(lastIndex, match.index) });
    }
    if (match[1] !== undefined) {
      parts.push({ type: 'thought', title: 'Thought', content: match[1] });
    } else {
      parts.push(parseToolUse(match[2] ?? ''));
    }
    lastIndex = match.index + match[0].length;
  }

  if (lastIndex < content.length) {
    parts.push({ type: 'text', content: content.slice(lastIndex) });
  }

  return parts;
}

function renderMarkdown(text: string) {
  return marked.parse(text, { async: false });
}

function openSource() {
  sourceOpen.value = true;
}

function closeSource() {
  sourceOpen.value = false;
}

interface ToolDisplay {
  name: string;
  args: Record<string, unknown> | null;
  result: string | null;
}

function stripToolResponseXML(s: string): string {
  return s.replace(/<tool_response[^>]*>([\s\S]*?)<\/tool_response>/g, '$1').trim();
}

// Build per-call display entries from tool_call/tool_result transcript messages.
// Native mode: tool_call message carries a tool_calls[] array; pair each entry with
// its matching tool_result by tool_call_id.
// XML mode: tool_call messages have no structured array; use tool_result messages
// directly (tool_name + stripped content).
const toolDisplays = computed((): ToolDisplay[] => {
  const msgs = props.toolMessages ?? [];
  if (msgs.length === 0) return [];

  const callMsgs = msgs.filter((m) => m.type === 'tool_call');
  const resultMsgs = msgs.filter((m) => m.type === 'tool_result');

  const displays: ToolDisplay[] = [];

  // Native mode: structured tool_calls present
  const nativeCalls = callMsgs.flatMap((m) => m.tool_calls ?? []);
  if (nativeCalls.length > 0) {
    for (const tc of nativeCalls) {
      let args: Record<string, unknown> | null = null;
      try { args = JSON.parse(tc.arguments); } catch { /* ignore */ }
      const resultMsg = resultMsgs.find((r) => r.tool_call_id === tc.id);
      displays.push({
        name: tc.tool_name,
        args,
        result: resultMsg ? stripToolResponseXML(resultMsg.content) : null,
      });
    }
    return displays;
  }

  // XML mode: use result messages (tool_name + content)
  for (const r of resultMsgs) {
    displays.push({
      name: r.tool_name ?? 'tool',
      args: null,
      result: stripToolResponseXML(r.content),
    });
  }
  return displays;
});

// Strip embedded <tool_use> blocks from content when we have actual tool messages
// so they don't duplicate the toolDisplays rendered above.
const displayContent = computed(() => {
  if ((props.toolMessages?.length ?? 0) === 0) return props.message.content;
  return props.message.content.replace(/<tool_use>[\s\S]*?<\/tool_use>\n?/g, '').trimStart();
});
</script>

<template>
  <article class="msg" :class="{ mine: props.mine, other: !props.mine, system: props.message.sender.type === 'system' }">
    <div class="bubble">
      <details
        v-for="(td, i) in toolDisplays"
        :key="i"
        class="tool-use"
      >
        <summary>Used {{ td.name }}</summary>
        <div v-if="td.args" class="tool-body">
          <pre class="tool-args">{{ JSON.stringify(td.args, null, 2) }}</pre>
        </div>
        <div v-if="td.result" class="tool-body tool-result">{{ td.result }}</div>
      </details>
      <template v-for="part in parseContent(displayContent)">
        <details v-if="part.type === 'thought'" class="thought">
          <summary>{{ part.title }}</summary>
          <p>{{ part.content }}</p>
        </details>
        <details v-else-if="part.type === 'tool_use'" class="tool-use">
          <summary>Used {{ (part as ToolUsePart).name }}</summary>
          <div v-if="(part as ToolUsePart).args" class="tool-body">
            <pre class="tool-args">{{ JSON.stringify((part as ToolUsePart).args, null, 2) }}</pre>
          </div>
          <div v-if="(part as ToolUsePart).result" class="tool-body tool-result">{{ (part as ToolUsePart).result }}</div>
        </details>
        <div v-else-if="!props.mine" class="content" v-html="renderMarkdown(part.content)" />
        <p v-else class="content">{{ part.content }}</p>
      </template>
      <div class="speaker">
        <span class="name">{{ props.message.sender.name }}</span>
        <div class="side">
          <button class="time-btn" type="button" @click="openSource">View Source</button>
          <span class="time">Copy</span>
          <span class="time">{{ props.timestampLabel }}</span>
        </div>
      </div>
    </div>
    <RoomMessageSourceModal :open="sourceOpen" :message="props.message" @close="closeSource" />
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

.msg.other .content {
  font-family: var(--font-body-serif);
  font-size: var(--body-size);
}

.msg.other.system .content {
  font-family: var(--code-family);
}

.msg.other:hover .bubble {
  background: var(--bg-secondary);
}

.msg.mine .bubble {
  border: 1px solid var(--border-subtle);
  background: var(--bg-secondary);
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

.msg details.thought,
.msg details.tool-use {
  border-radius: 0.75rem;
  color: var(--fg-muted);
  font-family: var(--body-family);
  cursor: pointer;
}

.msg details.thought summary,
.msg details.tool-use summary {
  color: var(--fg-muted);
  font-size: var(--body-xs-size);
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
