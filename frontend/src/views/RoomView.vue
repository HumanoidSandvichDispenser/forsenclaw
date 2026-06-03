<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';

import RoomComposer from '@/components/room/RoomComposer.vue';
import ConfirmationBanner from '@/components/room/ConfirmationBanner.vue';
import RoomHeader from '@/components/room/RoomHeader.vue';
import ContextPreviewModal from '@/components/room/ContextPreviewModal.vue';
import AgentDAGModal from '@/components/room/AgentDAGModal.vue';
import RoomMessageItem from '@/components/room/RoomMessageItem.vue';
import type { MessageResponse } from '@/client';
import type {
  MessageCreatedPayload,
  MessageDeltaPayload,
  ConfirmationPendingPayload,
  AgentErrorPayload,
} from '@/composables/useWebSocket';
import { useWebSocket } from '@/composables/useWebSocket';
import { useConfirmationsStore } from '@/stores/confirmations';
import { useMessagesStore } from '@/stores/messages';
import { useRoomsStore } from '@/stores/rooms';
import { useUserStore } from '@/stores/user';
import { agentNameFromId, isAgentId, isUserId, userId } from '@/utils/actor';

interface MessageGroup {
  key: string;
  message: MessageResponse;
  toolMessages: MessageResponse[];
}

const route = useRoute();
const roomsStore = useRoomsStore();
const messagesStore = useMessagesStore();
const confirmationsStore = useConfirmationsStore();
const userStore = useUserStore();
const ws = useWebSocket();

const roomId = computed(() => String(route.params.roomId ?? ''));
const room = computed(() => roomsStore.byId.get(Number(roomId.value)) ?? null);
const messageGroups = computed((): MessageGroup[] => {
  const msgs = messagesStore.byRoomId[roomId.value] ?? [];
  const groups: MessageGroup[] = [];
  let toolBuffer: MessageResponse[] = [];

  for (const m of msgs) {
    if (m.type === 'tool_call' || m.type === 'tool_result') {
      toolBuffer.push(m);
    } else {
      groups.push({ key: String(m.id), message: m, toolMessages: toolBuffer });
      toolBuffer = [];
    }
  }

  return groups;
});
const streaming = computed(() => messagesStore.streamingByRoomId[roomId.value] ?? null);

// liveStreaming keeps the streaming component mounted briefly after streaming
// ends so the close animations on thought blocks have time to play out.
const liveStreaming = ref(streaming.value);
let lingerTimer: ReturnType<typeof setTimeout> | null = null;
watch(streaming, (next) => {
  if (next != null) {
    if (lingerTimer) {
      clearTimeout(lingerTimer);
      lingerTimer = null;
    }
    liveStreaming.value = next;
  } else {
    lingerTimer = setTimeout(() => {
      liveStreaming.value = null;
      lingerTimer = null;
    }, 400);
  }
});

const messageText = ref('');
const sending = ref(false);
const composerError = ref<string | null>(null);

const scrollerEl = ref<HTMLElement | null>(null);

const meActorId = computed(() => {
  const participants = room.value?.participants ?? [];
  const byName = userStore.user?.name ? userId(userStore.user.name) : null;
  if (byName && participants.some((p) => p.id === byName)) return byName;
  const firstUser = participants.find((p) => isUserId(p.id));
  return firstUser?.id ?? byName ?? userId('local');
});

const members = computed(() => room.value?.participants ?? []);

const showPreview = ref(false);
const showDAG = ref(false);

// The agent to preview. Prefer a participant, but fall back to scanning message
// senders so the affordance works even if participants haven't loaded.
const previewAgentName = computed(() => {
  const p = (room.value?.participants ?? []).find((m) => isAgentId(m.id));
  if (p) return agentNameFromId(p.id);
  const msgs = messagesStore.byRoomId[roomId.value] ?? [];
  const m = msgs.find((msg) => msg.sender?.id && isAgentId(msg.sender.id));
  return m ? agentNameFromId(m.sender.id) : '';
});

const isLoading = computed(() => {
  return messagesStore.loadingByRoomId[roomId.value] && messageGroups.value.length === 0;
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
  await confirmationsStore.fetch(roomId.value);
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
    switch (event.type) {
      case 'message.created': {
        const p = event.payload as MessageCreatedPayload;
        if (String(p.room_id) !== roomId.value) return;
        const msg: MessageResponse = {
          id: p.id,
          timestamp: p.timestamp,
          room_id: p.room_id,
          sender: p.sender,
          clearance_tag: p.clearance_tag,
          type: p.type,
          content: p.content,
          tool_calls: p.tool_calls,
          tool_call_id: p.tool_call_id,
          tool_name: p.tool_name,
        };
        if (p.type === 'tool_call') {
          messagesStore.appendMessage(roomId.value, msg);
          for (const tc of p.tool_calls ?? []) {
            messagesStore.setToolCall(roomId.value, tc.tool_name);
          }
        } else if (p.type === 'tool_result') {
          messagesStore.appendMessage(roomId.value, msg);
          messagesStore.clearToolCall(roomId.value, p.tool_name);
        } else {
          messagesStore.finalizeMessage(roomId.value, msg);
        }
        break;
      }
      case 'message.delta': {
        const p = event.payload as MessageDeltaPayload;
        if (String(p.room_id) !== roomId.value) return;
        if (!messagesStore.streamingByRoomId[roomId.value]) {
          messagesStore.startTyping(roomId.value, p.actor);
        }
        messagesStore.appendChunk(roomId.value, p.delta);
        break;
      }
      case 'confirmation.pending': {
        const p = event.payload as ConfirmationPendingPayload;
        if (String(p.room_id) !== roomId.value) return;
        confirmationsStore.add(roomId.value, {
          node_id: p.node_id,
          agent_name: p.agent_name,
          room_id: p.room_id,
          tool_name: p.tool_name,
          args: p.args,
          reason: p.reason,
        });
        break;
      }
      case 'agent.error': {
        const p = event.payload as AgentErrorPayload;
        if (String(p.room_id) !== roomId.value) return;
        messagesStore.failTurn(roomId.value, `${p.agent_name} failed to respond: ${p.message}`);
        break;
      }
    }
  });
});

onBeforeUnmount(() => {
  if (roomId.value) {
    ws.unsubscribe(roomId.value);
  }
  if (unsubEvent) unsubEvent();
  if (lingerTimer) clearTimeout(lingerTimer);
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
  () => messageGroups.value.length,
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
      :participant-count="members.length"
      :agent-name="previewAgentName"
      @settings="() => {}"
      @preview="showPreview = true"
      @dag="showDAG = true"
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
              v-for="g in messageGroups"
              :key="g.key"
              :message="g.message"
              :tool-messages="g.toolMessages"
              :mine="g.message.sender.id === meActorId"
              :timestamp-label="formatTime(g.message.timestamp)"
            />
            <RoomMessageItem v-if="liveStreaming" :streaming="streaming ?? undefined" />
            <p v-if="messagesStore.turnErrorByRoomId[roomId]" class="turn-error" role="alert">
              {{ messagesStore.turnErrorByRoomId[roomId] }}
            </p>
          </div>
        </div>

        <div v-if="(confirmationsStore.byRoomId[roomId] ?? []).length > 0" class="confirmations">
          <ConfirmationBanner
            v-for="c in confirmationsStore.byRoomId[roomId]"
            :key="c.node_id"
            :room-id="roomId"
            :confirmation="c"
          />
        </div>

        <RoomComposer
          v-model="messageText"
          :error="composerError"
          :sending="sending"
          @submit="send"
        />
      </section>
    </main>

    <ContextPreviewModal
      v-if="showPreview && previewAgentName"
      :open="showPreview"
      :room-id="roomId"
      :agent-name="previewAgentName"
      @close="showPreview = false"
    />

    <AgentDAGModal
      v-if="showDAG && previewAgentName"
      :open="showDAG"
      :agent-name="previewAgentName"
      @close="showDAG = false"
    />
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
  grid-template-columns: 1fr;
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

.confirmations {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  padding: 0.75rem 1.25rem;
  border-top: 1px solid var(--border, #333);
}

.muted {
  color: var(--fg-tertiary);
  margin: 0;
}

.error {
  color: var(--error-dark);
  margin: 0;
}

.turn-error {
  margin: 0.5rem 0;
  padding: 0.5rem 0.75rem;
  border: 1px solid var(--error);
  border-radius: var(--border-radius);
  background: var(--error-light);
  color: var(--error-dark);
  font-size: var(--body-sm-size);
}
</style>
