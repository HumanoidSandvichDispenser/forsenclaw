<script setup lang="ts">
import type { MessageResponse } from '@/client';

defineProps<{
  message: MessageResponse;
  mine: boolean;
  timestampLabel: string;
}>();

// TODO: use xml and markdown parsers, for markdown output and structured
// content beyond thought tags like images, files, tool calls, etc.
function parseContent(content: string) {
  const parts = []
  const regex = /<thought>([\s\S]*?)<\/thought>/g
  let lastIndex = 0
  let match

  while ((match = regex.exec(content)) !== null) {
    if (match.index > lastIndex) {
      parts.push({ type: 'text', content: content.slice(lastIndex, match.index) })
    }
    parts.push({ type: 'thought', title: 'Thought', content: match[1] })
    lastIndex = match.index + match[0].length
  }

  if (lastIndex < content.length) {
    parts.push({ type: 'text', content: content.slice(lastIndex) })
  }

  return parts
}
</script>

<template>
  <article class="msg" :class="{ mine, other: !mine, system: message.sender.type === 'system' }">
    <div class="bubble">
      <template v-for="part in parseContent(message.content)">
        <details v-if="part.type === 'thought'" class="thought">
          <summary>{{ part.title }}</summary>
          <!--div v-html="renderMarkdown(part.content)" /-->
          <p>{{ part.content }}</p>
        </details>
        <p v-else class="content">{{ part.content }}</p>
      </template>
      <div class="speaker">
        <span class="name">{{ message.sender.name }}</span>
        <div class="side">
          <span class="time">View Source</span>
          <span class="time">Copy</span>
          <span class="time">{{ timestampLabel }}</span>
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

.msg.other p.content {
  font-family: var(--font-body-serif);
}

.msg.other.system p.content {
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

.time {
  color: var(--fg-muted);
}
</style>
