<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';

import { useMessagesStore } from '@/stores/messages';
import { useRoomsStore } from '@/stores/rooms';
import { useUserStore } from '@/stores/user';

const route = useRoute();
const roomsStore = useRoomsStore();
const messagesStore = useMessagesStore();
const userStore = useUserStore();

const roomId = computed(() => String(route.params.roomId ?? ''));
const room = computed(() => roomsStore.byId.get(roomId.value) ?? null);
const messages = computed(() => messagesStore.byRoomId[roomId.value] ?? []);

const messageText = ref('');
const sending = ref(false);
const composerError = ref<string | null>(null);

const scrollerEl = ref<HTMLElement | null>(null);

const meActorId = computed(() => {
  const participants = room.value?.participants ?? [];
  const byName = userStore.user?.name ? `user:${userStore.user.name}` : null;
  if (byName && participants.some((p) => p.id === byName)) return byName;
  const firstUser = participants.find((p) => p.id.startsWith('user:'));
  return firstUser?.id ?? byName ?? 'user:local';
});

const members = computed(() => room.value?.participants ?? []);

function formatTime(iso: string) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
}

async function scrollToBottom() {
  await nextTick();
  const el = scrollerEl.value;
  if (!el) return;
  el.scrollTop = el.scrollHeight;
}

async function ensureLoaded() {
  if (!roomId.value) return;
  if (!userStore.user && !userStore.loading) userStore.fetchMe();
  if (!room.value) roomsStore.fetchRoom(roomId.value);
  await messagesStore.fetchMessages(roomId.value);
  await scrollToBottom();
}

let poll: number | null = null;

onMounted(() => {
  ensureLoaded();
  poll = window.setInterval(() => {
    if (!roomId.value) return;
    messagesStore.fetchMessages(roomId.value);
  }, 2000);
});

onBeforeUnmount(() => {
  if (poll) window.clearInterval(poll);
});

watch(roomId, () => ensureLoaded());

watch(
  () => messages.value.length,
  async () => {
    const el = scrollerEl.value;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    // Only autoscroll if user is already near bottom.
    if (distanceFromBottom < 120) await scrollToBottom();
  },
);

async function send() {
  const trimmed = messageText.value.trim();
  if (!trimmed) return;
  if (!roomId.value) return;
  composerError.value = null;
  sending.value = true;
  try {
    await messagesStore.postMessage(roomId.value, meActorId.value, trimmed);
    messageText.value = '';
    await scrollToBottom();
  } catch (e) {
    composerError.value = e instanceof Error ? e.message : String(e);
  } finally {
    sending.value = false;
  }
}
</script>

<template>
  <section class="room-layout">
    <header class="room-header">
      <div class="title-block">
        <h1 class="h2 title">{{ (members.map((m) => m.name).join(' · ')) || roomId }}</h1>
        <p class="meta">
          <span class="text-tertiary">{{ room?.protocol_type || 'room' }}</span>
          <span class="dot" aria-hidden="true">•</span>
          <span class="text-tertiary">{{ members.length }} participants</span>
        </p>
      </div>

      <button class="settings" type="button" aria-label="Room settings">⚙</button>
    </header>

    <main class="room-main">
      <section class="chat">
        <div ref="scrollerEl" class="messages" aria-label="Messages">
          <p v-if="messagesStore.loadingByRoomId[roomId]" class="muted">Loading…</p>
          <p v-else-if="messagesStore.errorByRoomId[roomId]" class="error">{{ messagesStore.errorByRoomId[roomId] }}</p>

          <article
            v-for="m in messages"
            :key="m.id"
            class="msg"
            :class="{ mine: m.sender.id === meActorId }"
          >
            <div class="bubble">
              <p class="body">{{ m.content }}</p>
              <p class="speaker">
                <span class="name">{{ m.sender.name }}</span>
                <span class="time">{{ formatTime(m.timestamp) }}</span>
              </p>
            </div>
          </article>
        </div>

        <form class="composer" @submit.prevent="send">
          <input
            v-model="messageText"
            class="input"
            type="text"
            placeholder="Message…"
            :disabled="sending"
          />
          <button class="send" type="submit" :disabled="sending || !messageText.trim()">→</button>
          <p v-if="composerError" class="error composer-error">{{ composerError }}</p>
        </form>
      </section>

      <aside class="right">
        <h2 class="right-title">Members</h2>
        <div class="members">
          <div v-for="m in members" :key="m.id" class="member">
            <div class="member-name">
              <span>{{ m.name }}</span>
              <span v-if="m.id === meActorId" class="you">You</span>
            </div>
            <div class="member-meta">{{ m.type }}</div>
          </div>
        </div>

        <h2 class="right-title">Memory</h2>
        <div class="memory">
          <p class="muted">Not implemented yet.</p>
        </div>
      </aside>
    </main>
  </section>
</template>

<style scoped>
.room-layout {
  display: flex;
  flex-direction: column;
  height: 100vh;
}

.room-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 1rem 1.25rem;
  border-bottom: 1px solid var(--border-subtle);
  background: var(--bg-elevated);
}

.title-block {
  min-width: 0;
}

.title {
  margin: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.meta {
  margin: 0.25rem 0 0;
  font-size: var(--body-sm-size);
}

.dot {
  margin: 0 0.5rem;
  color: var(--fg-muted);
}

.settings {
  width: 2.25rem;
  height: 2.25rem;
  display: grid;
  place-items: center;
}

.room-main {
  display: grid;
  grid-template-columns: 1fr 18rem;
  min-height: 0;
  flex: 1;
}

.chat {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.messages {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 1.25rem;
  background: var(--bg-primary);
  display: grid;
  gap: 0.75rem;
}

.msg {
  display: flex;
}

.msg.mine {
  justify-content: flex-end;
}

.bubble {
  width: min(48rem, 100%);
  border: 1px solid var(--border-subtle);
  background: var(--bg-elevated);
  border-radius: 0.9rem;
  padding: 0.75rem 0.9rem;
}

.msg.mine .bubble {
  background: var(--bg-secondary);
}

.body {
  margin: 0;
  white-space: pre-wrap;
}

.speaker {
  margin: 0.5rem 0 0;
  display: flex;
  gap: 0.5rem;
  align-items: baseline;
  justify-content: space-between;
  color: var(--fg-tertiary);
  font-size: var(--body-xs-size);
}

.name {
  font-weight: 600;
}

.time {
  color: var(--fg-muted);
}

.composer {
  border-top: 1px solid var(--border-subtle);
  background: var(--bg-elevated);
  padding: 0.75rem 1.25rem;
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 0.75rem;
  align-items: center;
}

.input {
  padding: 0.7rem 0.9rem;
  border-radius: 0.6rem;
  border: 1px solid var(--border-default);
  background: var(--bg-primary);
}

.send {
  width: 2.4rem;
  height: 2.4rem;
  padding: 0;
}

.composer-error {
  grid-column: 1 / -1;
  margin: 0;
}

.right {
  border-left: 1px solid var(--border-subtle);
  background: var(--bg-elevated);
  padding: 1rem;
  overflow-y: auto;
  min-height: 0;
}

.right-title {
  margin: 0;
  color: var(--fg-tertiary);
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.members {
  margin-top: 0.75rem;
  display: grid;
  gap: 0.5rem;
  padding-bottom: 1rem;
  border-bottom: 1px solid var(--border-subtle);
}

.member {
  padding: 0.5rem 0.6rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.75rem;
  background: var(--bg-primary);
}

.member-name {
  display: flex;
  justify-content: space-between;
  gap: 0.75rem;
}

.you {
  color: var(--fg-tertiary);
  font-size: var(--body-xs-size);
}

.member-meta {
  color: var(--fg-tertiary);
  font-size: var(--body-xs-size);
  margin-top: 0.15rem;
}

.memory {
  margin-top: 0.75rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.75rem;
  background: var(--bg-primary);
  padding: 0.75rem;
}

.muted {
  color: var(--fg-tertiary);
  margin: 0;
}

.error {
  color: var(--error-dark);
  margin: 0;
}

@media (max-width: 900px) {
  .room-main {
    grid-template-columns: 1fr;
  }

  .right {
    display: none;
  }
}
</style>
