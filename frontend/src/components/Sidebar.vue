<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';

import SidebarList from '@/components/SidebarList.vue';
import SidebarTab from '@/components/SidebarTab.vue';
import CreateRoomModal from '@/components/CreateRoomModal.vue';

import { useAgentsStore } from '@/stores/agents';
import { roomAgentName, roomName, useRoomsStore } from '@/stores/rooms';

const router = useRouter();
const roomsStore = useRoomsStore();
const agentsStore = useAgentsStore();

const createOpen = ref(false);

onMounted(() => {
  roomsStore.fetchRooms();
  agentsStore.fetchAgents();
});

const roomTabs = computed(() =>
  roomsStore.rooms
    .filter((r) => Boolean(r && r.id))
    .map((r) => ({
      id: r.id,
      title: roomName(r),
      agent: roomAgentName(r),
      clearance: r.clearance,
    })),
);

const agentTabs = computed(() =>
  agentsStore.agents
    .filter((a) => a.active)
    .map((a) => ({
      name: a.name,
      label: a.name,
    })),
);

function onRoomCreated(roomId: string) {
  router.push(`/rooms/${roomId}`);
}
</script>

<template>
  <aside class="sidebar">
    <h3 class="title display-2 padding">forsenClaw</h3>
    <div class="title-divider" aria-hidden="true"></div>

    <div class="group">
      <div class="padding">
        <button class="create-room-btn" @click="createOpen = true">+ New Room</button>
      </div>

      <div class="group scrollable-container">
        <SidebarList label="Rooms" class="padding">
          <SidebarTab v-for="r in roomTabs" :key="r.id" :to="`/rooms/${r.id}`">
            <span class="room-title">{{ r.title }}</span>
            <span class="room-subtitle text-tertiary">
              <span v-if="r.agent" class="room-agent">{{ r.agent }}</span>
              <span class="room-clearance">Clearance {{ r.clearance }}</span>
            </span>
          </SidebarTab>
        </SidebarList>

        <SidebarList label="Agents" class="padding">
          <SidebarTab v-for="a in agentTabs" :key="a.name" :to="`/agents/${a.name}`">
            {{ a.label }}
          </SidebarTab>
        </SidebarList>
      </div>
    </div>

    <CreateRoomModal :open="createOpen" @close="createOpen = false" @created="onRoomCreated" />
  </aside>
</template>

<style scoped>
.sidebar {
  display: flex;
  flex-direction: column;
  width: 16rem;
  height: 100vh;
  border-right: 1px solid var(--border-subtle);
  background: var(--bg-elevated);
}

.sidebar .display-2 {
  margin: 1rem 0;
  font-size: var(--h2-size);
}

.title-divider {
  border-bottom: 1px solid var(--border-subtle);
  margin-bottom: 1rem;
}

.group {
  display: flex;
  flex-direction: column;
  gap: 2rem;
  flex: 1;
  min-height: 0;
}

.group.scrollable-container {
  overflow-y: auto;
  padding-bottom: 1rem;
  min-height: 0;
  flex: 1;
}

.padding {
  padding-left: 1rem;
  padding-right: 1rem;
}

.create-room-btn {
  display: block;
  width: 100%;
}

.room-title {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
}

.room-subtitle {
  display: flex;
  align-items: center;
  gap: 0.375rem;
  font-size: var(--body-sm-size);
  overflow: hidden;
}

.room-subtitle > span {
  overflow: hidden;
  text-overflow: ellipsis;
}

.room-agent + .room-clearance::before {
  content: '·';
  margin-right: 0.375rem;
  color: var(--fg-muted);
}
</style>
