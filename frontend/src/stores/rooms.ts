import { ref } from 'vue';
import { defineStore } from 'pinia';

export type Room = {
  id: string;
  name: string;
};

export const useRoomsStore = defineStore('rooms', () => {
  const rooms = ref<Room[]>([]);
  const activeRoomId = ref<string | null>(null);

  function setRooms(nextRooms: Room[]) {
    rooms.value = nextRooms;
  }

  function setActiveRoom(id: string | null) {
    activeRoomId.value = id;
  }

  return { rooms, activeRoomId, setRooms, setActiveRoom };
});
