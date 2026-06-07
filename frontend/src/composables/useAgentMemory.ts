import { ref, watch, type Ref } from 'vue';

import { getAgentMemory, getCompactionStats, compactRoom } from '@/client';
import type { AgentMemoryLevelResponse, CompactionStatsResponseBody } from '@/client';
import { useClientStore } from '@/stores/client';

// useAgentMemory loads an agent's per-clearance memory + daily notes for the
// inspection dock, alongside the room's compaction stats (cursor, live-transcript
// size), and exposes a compact action (POST /compact) that refreshes both so the
// resulting summary and the advanced cursor show up. The backend gates the memory
// read by clearance (403 → forbidden), mirroring the DAG panel.
export function useAgentMemory(agentName: Ref<string>, roomId: Ref<number>) {
  const levels = ref<AgentMemoryLevelResponse[]>([]);
  const stats = ref<CompactionStatsResponseBody | null>(null);
  const loading = ref(false);
  const error = ref('');
  const forbidden = ref(false);
  const compacting = ref(false);

  async function loadStats() {
    if (!agentName.value || !roomId.value) {
      stats.value = null;
      return;
    }
    try {
      const res = (await getCompactionStats({
        client: useClientStore().client,
        path: { room_id: roomId.value },
        query: { agent: agentName.value },
      })) as { data?: CompactionStatsResponseBody };
      stats.value = res?.data ?? null;
    } catch {
      stats.value = null;
    }
  }

  async function refresh() {
    if (!agentName.value) {
      levels.value = [];
      stats.value = null;
      return;
    }
    loading.value = true;
    error.value = '';
    forbidden.value = false;
    try {
      const res = (await getAgentMemory({
        client: useClientStore().client,
        path: { agent_name: agentName.value },
      })) as { data?: { levels?: AgentMemoryLevelResponse[] }; response?: Response };
      if (res?.response?.status === 403) {
        forbidden.value = true;
        return;
      }
      levels.value = res?.data?.levels ?? [];
      await loadStats();
    } catch (e) {
      const status =
        (e as { response?: { status?: number }; status?: number })?.response?.status ??
        (e as { status?: number })?.status;
      if (status === 403) forbidden.value = true;
      else error.value = e instanceof Error ? e.message : 'failed to load memory';
    } finally {
      loading.value = false;
    }
  }

  // targetBytes omitted (or 0) lets the backend fall back to the configured
  // automatic target.
  async function compact(targetBytes?: number) {
    if (!agentName.value || !roomId.value) return;
    compacting.value = true;
    error.value = '';
    try {
      await compactRoom({
        client: useClientStore().client,
        path: { room_id: roomId.value },
        body: {
          agent: agentName.value,
          ...(targetBytes && targetBytes > 0 ? { target: targetBytes } : {}),
        },
      });
      await refresh();
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'compaction failed';
    } finally {
      compacting.value = false;
    }
  }

  watch(agentName, refresh, { immediate: true });
  watch(roomId, loadStats);

  return { levels, stats, loading, error, forbidden, compacting, refresh, compact };
}
