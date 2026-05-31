import { computed, ref } from 'vue';
import { defineStore } from 'pinia';

import { listMessages, sendMessage, type ActorResponse, type MessageResponse } from '@/client';
import { useClientStore } from '@/stores/client';

export interface ToolCallEntry {
  name: string;
  done: boolean;
  args?: Record<string, unknown> | null;
  result?: string | null;
}

interface StreamingState {
  id: string;
  content: string;
  sender: ActorResponse;
  isStreaming: boolean;
  toolCalls: ToolCallEntry[];
}

export const useMessagesStore = defineStore('messages', () => {
  const clientStore = useClientStore();

  const byRoomId = ref<Record<string, MessageResponse[]>>({});
  const loadingByRoomId = ref<Record<string, boolean>>({});
  const errorByRoomId = ref<Record<string, string | null>>({});
  const streamingByRoomId = ref<Record<string, StreamingState | null>>({});

  const getMessages = (roomId: string) =>
    computed(() => byRoomId.value[roomId] ?? []);

  function setRoomLoading(roomId: string, value: boolean) {
    loadingByRoomId.value = { ...loadingByRoomId.value, [roomId]: value };
  }

  function setRoomError(roomId: string, value: string | null) {
    errorByRoomId.value = { ...errorByRoomId.value, [roomId]: value };
  }

  async function fetchMessages(roomId: string) {
    setRoomLoading(roomId, true);
    setRoomError(roomId, null);
    try {
      const res = await listMessages({
        client: clientStore.client,
        path: { room_id: roomId },
        query: { limit: 200 },
        throwOnError: true,
      });
      const raw = (res as any)?.data?.messages ?? (res as any)?.messages ?? [];
      const msgs = (raw as MessageResponse[])
        .slice()
        .sort((a: MessageResponse, b: MessageResponse) => a.timestamp.localeCompare(b.timestamp));
      byRoomId.value = { ...byRoomId.value, [roomId]: msgs };
    } catch (e) {
      setRoomError(roomId, e instanceof Error ? e.message : String(e));
    } finally {
      setRoomLoading(roomId, false);
    }
  }

  async function postMessage(roomId: string, sender: string, content: string) {
    setRoomError(roomId, null);
    const msg = await sendMessage({
      client: clientStore.client,
      path: { room_id: roomId },
      body: { sender, content },
      throwOnError: true,
    });
    const current = byRoomId.value[roomId] ?? [];
    const m = (msg as any)?.data ?? msg;
    byRoomId.value = { ...byRoomId.value, [roomId]: [...current, m] };
    return m;
  }

  function startTyping(roomId: string, sender: ActorResponse) {
    streamingByRoomId.value = {
      ...streamingByRoomId.value,
      [roomId]: {
        id: `streaming-${roomId}-${Date.now()}`,
        content: '',
        sender,
        isStreaming: true,
        toolCalls: [],
      },
    };
  }

  function appendChunk(roomId: string, content: string) {
    const current = streamingByRoomId.value[roomId];
    if (!current) return;
    streamingByRoomId.value = {
      ...streamingByRoomId.value,
      [roomId]: {
        ...current,
        content: current.content + content,
      },
    };
  }

  function setToolCall(roomId: string, toolName: string) {
    const current = streamingByRoomId.value[roomId];
    if (!current) return;
    streamingByRoomId.value = {
      ...streamingByRoomId.value,
      [roomId]: {
        ...current,
        toolCalls: [...current.toolCalls, { name: toolName, done: false }],
      },
    };
  }

  function clearToolCall(roomId: string, toolName?: string) {
    const current = streamingByRoomId.value[roomId];
    if (!current) return;
    let cleared = false;
    const toolCalls = current.toolCalls.map((tc) => {
      if (!cleared && !tc.done && (!toolName || tc.name === toolName)) {
        cleared = true;
        return { ...tc, done: true };
      }
      return tc;
    });
    streamingByRoomId.value = {
      ...streamingByRoomId.value,
      [roomId]: { ...current, toolCalls },
    };
  }

  // Append a message to the list without touching streaming state.
  function appendMessage(roomId: string, msg: MessageResponse) {
    const current = byRoomId.value[roomId] ?? [];
    byRoomId.value = { ...byRoomId.value, [roomId]: [...current, msg] };
  }

  function finalizeMessage(roomId: string, msg: MessageResponse) {
    streamingByRoomId.value = { ...streamingByRoomId.value, [roomId]: null };
    const current = byRoomId.value[roomId] ?? [];
    byRoomId.value = { ...byRoomId.value, [roomId]: [...current, msg] };
  }

  function clearStreaming(roomId: string) {
    streamingByRoomId.value = { ...streamingByRoomId.value, [roomId]: null };
  }

  function getStreaming(roomId: string) {
    return computed(() => streamingByRoomId.value[roomId] ?? null);
  }

  return {
    byRoomId,
    loadingByRoomId,
    errorByRoomId,
    streamingByRoomId,
    getMessages,
    getStreaming,
    fetchMessages,
    postMessage,
    startTyping,
    appendChunk,
    setToolCall,
    clearToolCall,
    appendMessage,
    finalizeMessage,
    clearStreaming,
  };
});
