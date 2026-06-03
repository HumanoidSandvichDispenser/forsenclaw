import { onUnmounted, ref } from 'vue';

import type { DagNode } from '@/client';
import { useClientStore } from '@/stores/client';

export type MessageCreatedPayload = {
  id: number;
  timestamp: string;
  room_id: number;
  sender: { id: string; name: string; type: string; clearance: number };
  clearance_tag: number;
  type: string;
  content: string;
  usage_input_tokens?: number;
  usage_output_tokens?: number;
  tool_calls?: Array<{ id?: string; tool_name: string; arguments?: string }>;
  tool_call_id?: string;
  tool_name?: string;
};

export type MessageDeltaPayload = {
  room_id: number;
  actor: { id: string; name: string; type: string; clearance: number };
  delta: string;
};

export type ConfirmationPendingPayload = {
  node_id: string;
  agent_name: string;
  room_id: number;
  tool_name: string;
  args: string;
  reason: string;
};

// A dag.update event carries a single DAG node-state transition for an agent.
// The payload is the same DagNode shape returned by the getAgentDag snapshot, so
// updates merge directly into a loaded snapshot.
export type DagUpdatePayload = DagNode;

export type WSEvent =
  | { type: 'message.created'; payload: MessageCreatedPayload }
  | { type: 'message.delta'; payload: MessageDeltaPayload }
  | { type: 'confirmation.pending'; payload: ConfirmationPendingPayload }
  | { type: 'dag.update'; payload: DagUpdatePayload }
  | { type: string; payload?: unknown };

type Callback = (event: WSEvent) => void;

export function useWebSocket() {
  const clientStore = useClientStore();

  const ws = ref<WebSocket | null>(null);
  const connected = ref(false);
  const reconnectAttempts = ref(0);
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  const callbacks: Callback[] = [];
  const pendingSubscribes = new Set<number>();
  const pendingAgentSubscribes = new Set<string>();

  function buildURL(): string {
    const base = clientStore.baseUrl.replace(/^http/, 'ws');
    return `${base}/api/ws`;
  }

  function connect() {
    if (ws.value) {
      ws.value.close();
    }

    const url = buildURL();
    const socket = new WebSocket(url);

    socket.onopen = () => {
      connected.value = true;
      reconnectAttempts.value = 0;
      for (const roomId of pendingSubscribes) {
        socket.send(JSON.stringify({ action: 'subscribe', room_id: roomId }));
      }
      for (const name of pendingAgentSubscribes) {
        socket.send(JSON.stringify({ action: 'subscribe_agent', agent: name }));
      }
    };

    socket.onclose = () => {
      connected.value = false;
      ws.value = null;
      scheduleReconnect();
    };

    socket.onerror = () => {
      // onclose will fire after onerror, reconnect handled there
    };

    socket.onmessage = (event: MessageEvent) => {
      try {
        const parsed: WSEvent = JSON.parse(event.data);
        for (const cb of callbacks) {
          cb(parsed);
        }
      } catch {
        // ignore malformed messages
      }
    };

    ws.value = socket;
  }

  function scheduleReconnect() {
    if (reconnectTimer) return;
    const delay = Math.min(1000 * 2 ** reconnectAttempts.value, 30000);
    reconnectAttempts.value++;
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      connect();
    }, delay);
  }

  function subscribe(roomId: string | number) {
    const id = Number(roomId);
    pendingSubscribes.add(id);
    if (!ws.value || ws.value.readyState !== WebSocket.OPEN) return;
    ws.value.send(JSON.stringify({ action: 'subscribe', room_id: id }));
  }

  function unsubscribe(roomId: string | number) {
    const id = Number(roomId);
    pendingSubscribes.delete(id);
    if (!ws.value || ws.value.readyState !== WebSocket.OPEN) return;
    ws.value.send(JSON.stringify({ action: 'unsubscribe', room_id: id }));
  }

  function subscribeAgent(name: string) {
    pendingAgentSubscribes.add(name);
    if (!ws.value || ws.value.readyState !== WebSocket.OPEN) return;
    ws.value.send(JSON.stringify({ action: 'subscribe_agent', agent: name }));
  }

  function unsubscribeAgent(name: string) {
    pendingAgentSubscribes.delete(name);
    if (!ws.value || ws.value.readyState !== WebSocket.OPEN) return;
    ws.value.send(JSON.stringify({ action: 'unsubscribe_agent', agent: name }));
  }

  function onEvent(cb: Callback) {
    callbacks.push(cb);
    return () => {
      const idx = callbacks.indexOf(cb);
      if (idx !== -1) callbacks.splice(idx, 1);
    };
  }

  function disconnect() {
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    if (ws.value) {
      ws.value.close();
      ws.value = null;
    }
    connected.value = false;
  }

  onUnmounted(() => {
    disconnect();
  });

  return {
    connect,
    disconnect,
    subscribe,
    unsubscribe,
    subscribeAgent,
    unsubscribeAgent,
    onEvent,
    connected,
  };
}
