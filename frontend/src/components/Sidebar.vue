<script setup lang="ts">
import SidebarList from '@/components/SidebarList.vue';
import SidebarTab from '@/components/SidebarTab.vue';
</script>

<template>
  <aside class="sidebar">
    <h3 class="title display-2 padding">forsenClaw</h3>
    <div class="title-divider" aria-hidden="true"></div>

    <div class="group">
      <div class="padding">
        <button class="create-room-btn">Create Room</button>
      </div>

      <div class="group scrollable-container">
        <SidebarList label="Rooms" class="padding">
          <SidebarTab to="/rooms/uuid-here">Shiet</SidebarTab>
        </SidebarList>

        <SidebarList label="Agents" class="padding">
          <SidebarTab to="/agents/xyz">xyz</SidebarTab>
          <SidebarTab to="/agents/xyz">xyz</SidebarTab>
          <SidebarTab to="/agents/xyz">xyz</SidebarTab>
          <SidebarTab to="/agents/xyz">xyz</SidebarTab>
          <SidebarTab to="/agents/xyz">xyz</SidebarTab>
          <SidebarTab to="/agents/xyz">xyz</SidebarTab>
          <SidebarTab to="/agents/xyz">xyz</SidebarTab>
          <SidebarTab to="/agents/xyz">xyz</SidebarTab>
          <SidebarTab to="/agents/xyz">xyz</SidebarTab>
          <SidebarTab to="/agents/xyz">xyz</SidebarTab>
          <SidebarTab to="/agents/xyz">xyz</SidebarTab>
          <SidebarTab to="/agents/xyz">xyz</SidebarTab>
          <SidebarTab to="/agents/xyz">xyz</SidebarTab>
          <SidebarTab to="/agents/xyz">xyz</SidebarTab>
          <SidebarTab to="/agents/xyz">xyz</SidebarTab>
          <SidebarTab to="/agents/xyz">xyz</SidebarTab>
          <SidebarTab to="/agents/xyz">xyz</SidebarTab>
          <SidebarTab to="/agents/xyz">xyz</SidebarTab>
        </SidebarList>
      </div>
    </div>
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
</style>
