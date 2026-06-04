import { computed, ref } from 'vue';
import { defineStore } from 'pinia';

import {
  listMessages,
  sendMessage,
  switchBranch,
  editMessage,
  retryMessage,
  type ActorResponse,
  type MessageResponse,
} from '@/client';
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
  // turnErrorByRoomId holds a failed agent turn's error, shown where the reply
  // would have appeared. Distinct from errorByRoomId (room load failures).
  const turnErrorByRoomId = ref<Record<string, string | null>>({});

  const getMessages = (roomId: string) => computed(() => byRoomId.value[roomId] ?? []);

  function setRoomLoading(roomId: string, value: boolean) {
    loadingByRoomId.value = { ...loadingByRoomId.value, [roomId]: value };
  }

  function setRoomError(roomId: string, value: string | null) {
    errorByRoomId.value = { ...errorByRoomId.value, [roomId]: value };
  }

  function setTurnError(roomId: string, value: string | null) {
    turnErrorByRoomId.value = { ...turnErrorByRoomId.value, [roomId]: value };
  }

  // failTurn surfaces a failed agent turn: stop the typing indicator and record
  // why so the room view can show it.
  function failTurn(roomId: string, message: string) {
    clearStreaming(roomId);
    setTurnError(roomId, message);
  }

  async function fetchMessages(roomId: string) {
    setRoomLoading(roomId, true);
    setRoomError(roomId, null);
    try {
      const res = await listMessages({
        client: clientStore.client,
        path: { room_id: Number(roomId) },
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
    setTurnError(roomId, null);
    const msg = await sendMessage({
      client: clientStore.client,
      path: { room_id: Number(roomId) },
      body: { sender, content },
      throwOnError: true,
    });
    const current = byRoomId.value[roomId] ?? [];
    const m = (msg as any)?.data ?? msg;
    byRoomId.value = { ...byRoomId.value, [roomId]: [...current, m] };
    return m;
  }

  function startTyping(roomId: string, sender: ActorResponse) {
    setTurnError(roomId, null);
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

  // upsertMessage appends a message, or replaces it in place when one with the
  // same id already exists. Branching re-fetches the active branch right when a
  // regenerated reply may also stream in; upserting by id keeps the two paths
  // idempotent instead of duplicating the message.
  function upsertMessage(roomId: string, msg: MessageResponse) {
    const current = byRoomId.value[roomId] ?? [];
    const idx = current.findIndex((m) => m.id === msg.id);
    const next = idx >= 0 ? current.map((m, i) => (i === idx ? msg : m)) : [...current, msg];
    byRoomId.value = { ...byRoomId.value, [roomId]: next };
  }

  // Append a message to the list without touching streaming state.
  function appendMessage(roomId: string, msg: MessageResponse) {
    upsertMessage(roomId, msg);
  }

  function finalizeMessage(roomId: string, msg: MessageResponse) {
    setTurnError(roomId, null);
    streamingByRoomId.value = { ...streamingByRoomId.value, [roomId]: null };
    upsertMessage(roomId, msg);
  }

  // switchBranchToMessage makes the message's branch active at its fork, then
  // reloads the (now different) active branch.
  async function switchBranchToMessage(roomId: string, messageId: number) {
    await switchBranch({
      client: clientStore.client,
      path: { room_id: Number(roomId), message_id: messageId },
      throwOnError: true,
    });
    await fetchMessages(roomId);
  }

  // editMessageBranch forks a sibling of the message with new content; the agent
  // re-responds on the new branch (arriving over the WS stream).
  async function editMessageBranch(roomId: string, messageId: number, content: string) {
    setTurnError(roomId, null);
    await editMessage({
      client: clientStore.client,
      path: { room_id: Number(roomId), message_id: messageId },
      body: { content },
      throwOnError: true,
    });
    await fetchMessages(roomId);
  }

  // retryMessageBranch regenerates an assistant message as a new sibling.
  async function retryMessageBranch(roomId: string, messageId: number) {
    setTurnError(roomId, null);
    await retryMessage({
      client: clientStore.client,
      path: { room_id: Number(roomId), message_id: messageId },
      throwOnError: true,
    });
    await fetchMessages(roomId);
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
    turnErrorByRoomId,
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
    failTurn,
    clearStreaming,
    switchBranchToMessage,
    editMessageBranch,
    retryMessageBranch,
  };
});
