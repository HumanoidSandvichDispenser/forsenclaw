<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';

import RoomComposer from '@/components/room/RoomComposer.vue';
import RoomHeader from '@/components/room/RoomHeader.vue';
import RoomMembersPanel from '@/components/room/RoomMembersPanel.vue';
import RoomMessageItem from '@/components/room/RoomMessageItem.vue';
import RoomStreamingMessageItem from '@/components/room/RoomStreamingMessageItem.vue';
import { useWebSocket } from '@/composables/useWebSocket';
import { useMessagesStore } from '@/stores/messages';
import { useRoomsStore } from '@/stores/rooms';
import { useUserStore } from '@/stores/user';

const route = useRoute();
const roomsStore = useRoomsStore();
const messagesStore = useMessagesStore();
const userStore = useUserStore();
const ws = useWebSocket();

const roomId = computed(() => String(route.params.roomId ?? ''));
const room = computed(() => roomsStore.byId.get(roomId.value) ?? null);
const messages = computed(() => messagesStore.byRoomId[roomId.value] ?? []);
const streaming = computed(() => messagesStore.streamingByRoomId[roomId.value] ?? null);

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

const agentSender = computed(() => {
  const participants = room.value?.participants ?? [];
  const agent = participants.find((p) => p.id.startsWith('agent:'));
  if (agent) return agent;
  return { id: 'agent:unknown', name: 'Agent', type: 'agent', clearance: 0 };
});

const isLoading = computed(() => {
  return messagesStore.loadingByRoomId[roomId.value] &&
    messages.value.length === 0;
});

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

let unsubEvent: (() => void) | null = null;

onMounted(() => {
  ensureLoaded();
  ws.connect();
  if (roomId.value) {
    ws.subscribe(roomId.value);
  }
  unsubEvent = ws.onEvent((event) => {
    if (event.room_id !== roomId.value) return;
    switch (event.type) {
      case 'typing':
        messagesStore.startTyping(roomId.value, agentSender.value);
        break;
      case 'chunk':
        if (event.content) {
          messagesStore.appendChunk(roomId.value, event.content);
        }
        break;
      case 'message':
        if (event.message) {
          const msg = {
            id: event.message.id,
            timestamp: event.message.timestamp,
            room_id: event.message.room_id,
            sender: {
              id: event.message.sender_id,
              name: event.message.sender_name,
              type: event.message.sender_type,
              clearance: event.message.clearance_tag,
            },
            clearance_tag: event.message.clearance_tag,
            type: event.message.type,
            content: event.message.content,
          };
          messagesStore.finalizeMessage(roomId.value, msg);
        }
        break;
      case 'agent_error':
        messagesStore.clearStreaming(roomId.value);
        break;
      case 'tool_call':
        if (event.content) {
          messagesStore.setToolCall(roomId.value, event.content);
        }
        break;
      case 'tool_result':
        messagesStore.clearToolCall(roomId.value);
        break;
    }
  });
});

onBeforeUnmount(() => {
  if (roomId.value) {
    ws.unsubscribe(roomId.value);
  }
  if (unsubEvent) unsubEvent();
});

watch(roomId, (newId, oldId) => {
  if (oldId) {
    ws.unsubscribe(oldId);
    messagesStore.clearStreaming(oldId);
  }
  if (newId) {
    ws.subscribe(newId);
    ensureLoaded();
  }
});

watch(
  () => messages.value.length,
  async () => {
    const el = scrollerEl.value;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    if (distanceFromBottom < 120) await scrollToBottom();
  },
);

watch(
  () => streaming.value?.content,
  async () => {
    const el = scrollerEl.value;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
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
    <RoomHeader
      :title="members.map((m) => m.name).join(' · ') || roomId"
      :protocol-type="room?.protocol_type || 'room'"
      :participant-count="members.length"
      @settings="() => {}"
    />

    <main class="room-main">
      <section class="chat">
        <div ref="scrollerEl" class="messages" aria-label="Messages">
          <div class="margins">
            <p v-if="isLoading" class="muted">Loading…</p>
            <p v-else-if="messagesStore.errorByRoomId[roomId]" class="error">
              {{ messagesStore.errorByRoomId[roomId] }}
            </p>

            <RoomMessageItem
              v-for="m in messages"
              :key="m.id"
              :message="m"
              :mine="m.sender.id === meActorId"
              :timestamp-label="formatTime(m.timestamp)"
            />
            <RoomStreamingMessageItem
              v-if="streaming"
              :content="streaming.content"
              :sender="streaming.sender"
              :tool-calls="streaming.toolCalls"
            />
          </div>
        </div>

        <RoomComposer
          v-model="messageText"
          :error="composerError"
          :sending="sending"
          @submit="send"
        />
      </section>

      <RoomMembersPanel :members="members" :me-actor-id="meActorId" />
    </main>
  </section>
</template>

<style scoped>
.room-layout {
  display: flex;
  flex-direction: column;
  height: 100vh;
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
  overflow-y: auto;
}

.messages {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 1.25rem;
}

.margins {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  max-width: 40rem;
  margin: 0 auto;
}

.muted {
  color: var(--fg-tertiary);
  margin: 0;
}

.error {
  color: var(--error-dark);
  margin: 0;
}
</style>
