import { computed, ref } from 'vue';
import { defineStore } from 'pinia';

import { createRoom, getRoom, listRooms, type RoomResponse } from '@/client';
import { useClientStore } from '@/stores/client';

export const useRoomsStore = defineStore('rooms', () => {
  const clientStore = useClientStore();

  const rooms = ref<RoomResponse[]>([]);
  const byId = computed(() => new Map(rooms.value.map((r) => [r.id, r])));

  const loading = ref(false);
  const error = ref<string | null>(null);

  async function fetchRooms() {
    loading.value = true;
    error.value = null;
    try {
      const res = await listRooms({
        client: clientStore.client,
        query: { limit: 200, offset: 0 },
        throwOnError: true,
      });
      rooms.value = (res as any)?.data?.rooms ?? (res as any)?.rooms ?? [];
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
    } finally {
      loading.value = false;
    }
  }

  async function fetchRoom(roomId: string) {
    error.value = null;
    try {
      const room = await getRoom({
        client: clientStore.client,
        path: { room_id: roomId },
        throwOnError: true,
      });
      const r = (room as any)?.data ?? room;
      const idx = rooms.value.findIndex((x) => x.id === r.id);
      if (idx === -1) rooms.value.unshift(r);
      else rooms.value[idx] = r;
      return r;
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
      return null;
    }
  }

  async function createFreeformRoom(participantIds: string[]) {
    error.value = null;
    // backend currently enforces exactly 2 participants.
    const room = await createRoom({
      client: clientStore.client,
      throwOnError: true,
      body: {
        clearance: 5,
        participant_ids: participantIds,
      },
    });
    const r = (room as any)?.data ?? room;
    rooms.value.unshift(r);
    return r;
  }

  return {
    rooms,
    byId,
    loading,
    error,
    fetchRooms,
    fetchRoom,
    createFreeformRoom,
  };
});
