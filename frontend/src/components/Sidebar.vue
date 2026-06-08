<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';

import SidebarList from '@/components/SidebarList.vue';
import SidebarTab from '@/components/SidebarTab.vue';
import CreateRoomModal from '@/components/CreateRoomModal.vue';

import { useAgentsStore } from '@/stores/agents';
import { roomAgentName, roomName, useRoomsStore } from '@/stores/rooms';
import { useSidebar } from '@/composables/useSidebar';

const router = useRouter();
const roomsStore = useRoomsStore();
const agentsStore = useAgentsStore();
// Destructured so `drawerOpen` is a top-level ref binding Vue auto-unwraps in
// the template; a nested `sidebar.open` would always be truthy.
const { open: drawerOpen } = useSidebar();

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
  <aside class="sidebar" :class="{ open: drawerOpen }">
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

/* Below the breakpoint the sidebar is an off-canvas drawer: fixed, slid out by
   default, and revealed (above the scrim) when the shared open state is set. */
@media (max-width: 768px) {
  .sidebar {
    position: fixed;
    top: 0;
    left: 0;
    z-index: 50;
    width: min(16rem, 82vw);
    transform: translateX(-100%);
    transition: transform 0.2s ease;
  }

  .sidebar.open {
    transform: none;
  }
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
