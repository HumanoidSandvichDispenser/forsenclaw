import { onUnmounted, ref } from 'vue';

import { useClientStore } from '@/stores/client';

export type WSEvent = {
  type: 'typing' | 'chunk' | 'message' | 'agent_error' | 'interjection_queued' | 'tool_call' | 'tool_result';
  room_id: string;
  content?: string;
  message?: {
    id: string;
    timestamp: string;
    room_id: string;
    sender_id: string;
    sender_name: string;
    sender_type: string;
    clearance_tag: number;
    type: string;
    content: string;
    usage?: {
      input_tokens: number;
      output_tokens: number;
    };
  };
};

type Callback = (event: WSEvent) => void;

export function useWebSocket() {
  const clientStore = useClientStore();

  const ws = ref<WebSocket | null>(null);
  const connected = ref(false);
  const reconnectAttempts = ref(0);
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  const callbacks: Callback[] = [];

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

  function subscribe(roomId: string) {
    if (!ws.value || ws.value.readyState !== WebSocket.OPEN) return;
    ws.value.send(JSON.stringify({ action: 'subscribe', room_id: roomId }));
  }

  function unsubscribe(roomId: string) {
    if (!ws.value || ws.value.readyState !== WebSocket.OPEN) return;
    ws.value.send(JSON.stringify({ action: 'unsubscribe', room_id: roomId }));
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
    onEvent,
    connected,
  };
}
