import { computed, ref } from 'vue';
import { defineStore } from 'pinia';

import { listMessages, sendMessage, type MessageResponse } from '@/client';
import { useClientStore } from '@/stores/client';

export const useMessagesStore = defineStore('messages', () => {
  const clientStore = useClientStore();

  const byRoomId = ref<Record<string, MessageResponse[]>>({});
  const loadingByRoomId = ref<Record<string, boolean>>({});
  const errorByRoomId = ref<Record<string, string | null>>({});

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
      });
      const msgs = (res.messages ?? []).slice().sort((a, b) => a.timestamp.localeCompare(b.timestamp));
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
    });
    const current = byRoomId.value[roomId] ?? [];
    byRoomId.value = { ...byRoomId.value, [roomId]: [...current, msg] };
    return msg;
  }

  return {
    byRoomId,
    loadingByRoomId,
    errorByRoomId,
    getMessages,
    fetchMessages,
    postMessage,
  };
});
