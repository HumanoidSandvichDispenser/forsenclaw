import { ref } from 'vue';
import { defineStore } from 'pinia';

import { listConfirmations, respondConfirmation } from '@/client';
import type { ConfirmationResponse } from '@/client/types.gen';
import { useClientStore } from '@/stores/client';

export const useConfirmationsStore = defineStore('confirmations', () => {
  const clientStore = useClientStore();

  // pending confirmations keyed by roomId string
  const byRoomId = ref<Record<string, ConfirmationResponse[]>>({});

  function add(roomId: string, c: ConfirmationResponse) {
    const current = byRoomId.value[roomId] ?? [];
    if (current.some((x) => x.node_id === c.node_id)) return;
    byRoomId.value = { ...byRoomId.value, [roomId]: [...current, c] };
  }

  function remove(roomId: string, nodeId: string) {
    const current = byRoomId.value[roomId] ?? [];
    byRoomId.value = {
      ...byRoomId.value,
      [roomId]: current.filter((c) => c.node_id !== nodeId),
    };
  }

  async function fetch(roomId: string) {
    const res = await listConfirmations({
      client: clientStore.client,
      path: { room_id: roomId },
    } as any);
    const data = (res as any)?.data ?? res;
    const items: ConfirmationResponse[] = data?.confirmations ?? [];
    byRoomId.value = { ...byRoomId.value, [roomId]: items };
  }

  async function respond(
    roomId: string,
    nodeId: string,
    action: 'allow' | 'deny' | 'revise',
    opts?: { args?: string; feedback?: string },
  ) {
    await respondConfirmation({
      client: clientStore.client,
      path: { room_id: roomId, node_id: nodeId },
      body: { action, ...opts },
    } as any);
    remove(roomId, nodeId);
  }

  return { byRoomId, add, remove, fetch, respond };
});
