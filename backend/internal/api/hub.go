package api

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dispatch"
)

// HubNotifier adapts Hub to satisfy agent.ConfirmationNotifier.
// Used in main.go to wire the hub into the agent manager before Service exists.
type HubNotifier struct{ hub *Hub }

// NewHubNotifier wraps a Hub as an agent.ConfirmationNotifier.
func NewHubNotifier(hub *Hub) *HubNotifier { return &HubNotifier{hub: hub} }

// NotifyConfirmationPending implements agent.ConfirmationNotifier.
func (n *HubNotifier) NotifyConfirmationPending(roomID int64, c agent.PendingConfirmation) {
	n.hub.Broadcast(roomID, dispatch.StreamEvent{
		Type:    "confirmation.pending",
		Payload: c,
	})
}

// HubDAGStream adapts Hub to satisfy agent.DAGStreamWriter, broadcasting a
// node-state transition to clients subscribed to the agent's DAG stream.
type HubDAGStream struct {
	hub *Hub
	// gate decides whether a DAG update for an agent may reach the viewer. nil
	// means allow all (tests). Set once at startup before serving via
	// SetClearanceGate, mirroring the create_room resolver wiring; no lock, since
	// no DAG activity occurs until a request is dispatched after startup.
	gate func(agentName string) bool
}

// NewHubDAGStream wraps a Hub as an agent.DAGStreamWriter.
func NewHubDAGStream(hub *Hub) *HubDAGStream { return &HubDAGStream{hub: hub} }

// SetClearanceGate installs the clearance predicate. Late-bound because the gate
// needs the agent manager, which is constructed after this stream.
func (s *HubDAGStream) SetClearanceGate(gate func(agentName string) bool) {
	s.gate = gate
}

// StreamDAGUpdate implements agent.DAGStreamWriter. It drops the update when the
// viewer is not cleared for the agent's DAG (same rule as the REST snapshot), so
// the stream can't leak the existence or timing of higher-clearance work.
func (s *HubDAGStream) StreamDAGUpdate(agentName string, node agent.DAGNode) {
	if s.gate != nil && !s.gate(agentName) {
		return
	}
	s.hub.BroadcastAgent(agentName, dispatch.StreamEvent{
		Type:    "dag.update",
		Payload: node,
	})
}

// Hub manages WebSocket client connections and broadcasts room events.
// Clients subscribe to two independent channels: rooms (for message/confirmation
// events) and agents (for the agent's DAG-viewer stream, which is per-agent and
// not room-scoped).
type Hub struct {
	mu               sync.RWMutex
	rooms            map[int64]map[*Client]struct{}  // room_id -> set of clients
	agents           map[string]map[*Client]struct{} // agent name -> set of clients
	register         chan *Client
	unregister       chan *Client
	unsubscribeRoom  chan roomUnsubscribeMsg
	unsubscribeAgent chan agentUnsubscribeMsg
	broadcast        chan broadcastMsg
	broadcastAgent   chan agentBroadcastMsg
}

type broadcastMsg struct {
	roomID int64
	data   []byte
}

type agentBroadcastMsg struct {
	agent string
	data  []byte
}

type roomUnsubscribeMsg struct {
	client *Client
	roomID int64
}

type agentUnsubscribeMsg struct {
	client *Client
	agent  string
}

// NewHub creates a new WebSocket hub.
func NewHub() *Hub {
	return &Hub{
		rooms:            make(map[int64]map[*Client]struct{}),
		agents:           make(map[string]map[*Client]struct{}),
		register:         make(chan *Client),
		unregister:       make(chan *Client),
		unsubscribeRoom:  make(chan roomUnsubscribeMsg),
		unsubscribeAgent: make(chan agentUnsubscribeMsg),
		broadcast:        make(chan broadcastMsg, 64),
		broadcastAgent:   make(chan agentBroadcastMsg, 64),
	}
}

// Run starts the hub's background goroutine that manages client
// registration, unregistration, and broadcast fan-out.
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.registerClient(client)

		case client := <-h.unregister:
			h.unregisterClient(client)

		case msg := <-h.unsubscribeRoom:
			h.unsubscribeClientFromRoom(msg.client, msg.roomID)

		case msg := <-h.unsubscribeAgent:
			h.unsubscribeClientFromAgent(msg.client, msg.agent)

		case msg := <-h.broadcast:
			h.broadcastToRoom(msg.roomID, msg.data)

		case msg := <-h.broadcastAgent:
			h.broadcastToAgent(msg.agent, msg.data)
		}
	}
}

// Broadcast sends a real-time event to all clients subscribed to the given room.
func (h *Hub) Broadcast(roomID int64, event dispatch.StreamEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("hub: failed to marshal event: %v", err)
		return
	}

	select {
	case h.broadcast <- broadcastMsg{roomID: roomID, data: data}:
	default:
		log.Printf("hub: broadcast channel full, dropping event for room %d", roomID)
	}
}

// BroadcastAgent sends a real-time event to all clients subscribed to the given
// agent's stream (e.g. DAG-viewer updates).
func (h *Hub) BroadcastAgent(agentName string, event dispatch.StreamEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("hub: failed to marshal agent event: %v", err)
		return
	}

	select {
	case h.broadcastAgent <- agentBroadcastMsg{agent: agentName, data: data}:
	default:
		log.Printf("hub: agent broadcast channel full, dropping event for agent %s", agentName)
	}
}

// registerClient adds a client to its subscribed rooms and agents.
func (h *Hub) registerClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for roomID := range client.rooms {
		if h.rooms[roomID] == nil {
			h.rooms[roomID] = make(map[*Client]struct{})
		}
		h.rooms[roomID][client] = struct{}{}
	}
	for name := range client.agents {
		if h.agents[name] == nil {
			h.agents[name] = make(map[*Client]struct{})
		}
		h.agents[name][client] = struct{}{}
	}
}

// unsubscribeClientFromRoom removes a single client from one room in the hub map.
func (h *Hub) unsubscribeClientFromRoom(client *Client, roomID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if clients, ok := h.rooms[roomID]; ok {
		delete(clients, client)
		if len(clients) == 0 {
			delete(h.rooms, roomID)
		}
	}
}

// unsubscribeClientFromAgent removes a single client from one agent's set.
func (h *Hub) unsubscribeClientFromAgent(client *Client, name string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if clients, ok := h.agents[name]; ok {
		delete(clients, client)
		if len(clients) == 0 {
			delete(h.agents, name)
		}
	}
}

// unregisterClient removes a client from all rooms and agents and closes its
// send channel.
func (h *Hub) unregisterClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for roomID := range client.rooms {
		if clients, ok := h.rooms[roomID]; ok {
			delete(clients, client)
			if len(clients) == 0 {
				delete(h.rooms, roomID)
			}
		}
	}
	for name := range client.agents {
		if clients, ok := h.agents[name]; ok {
			delete(clients, client)
			if len(clients) == 0 {
				delete(h.agents, name)
			}
		}
	}
	close(client.send)
}

// broadcastToRoom fans out a message to all clients subscribed to a room.
func (h *Hub) broadcastToRoom(roomID int64, data []byte) {
	h.mu.RLock()
	clients, ok := h.rooms[roomID]
	h.mu.RUnlock()

	if !ok {
		return
	}

	for client := range clients {
		select {
		case client.send <- data:
		default:
			// Client's send channel is full — drop the message for this client
			log.Printf("hub: client send channel full, dropping message")
		}
	}
}

// broadcastToAgent fans out a message to all clients subscribed to an agent.
func (h *Hub) broadcastToAgent(name string, data []byte) {
	h.mu.RLock()
	clients, ok := h.agents[name]
	h.mu.RUnlock()

	if !ok {
		return
	}

	for client := range clients {
		select {
		case client.send <- data:
		default:
			log.Printf("hub: client send channel full, dropping agent message")
		}
	}
}

// Client represents a single WebSocket connection.
type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	rooms  map[int64]struct{}  // subscribed room IDs
	agents map[string]struct{} // subscribed agent names (DAG streams)
	send   chan []byte

	// onSubscribe is called after a client successfully subscribes to a room.
	// Used to replay pending state (e.g. confirmation events) to newly connected clients.
	onSubscribe func(roomID int64)
}

// NewClient creates a new client for the given WebSocket connection.
func NewClient(hub *Hub, conn *websocket.Conn) *Client {
	return &Client{
		hub:    hub,
		conn:   conn,
		rooms:  make(map[int64]struct{}),
		agents: make(map[string]struct{}),
		send:   make(chan []byte, 256),
	}
}

// Subscribe adds a room to this client's subscription set.
func (c *Client) Subscribe(roomID int64) {
	c.rooms[roomID] = struct{}{}
	c.hub.register <- c
}

// Unsubscribe removes a room from this client's subscription set and notifies
// the hub so it stops delivering events for that room to this client.
func (c *Client) Unsubscribe(roomID int64) {
	delete(c.rooms, roomID)
	c.hub.unsubscribeRoom <- roomUnsubscribeMsg{client: c, roomID: roomID}
}

// SubscribeAgent adds an agent's DAG stream to this client's subscription set.
func (c *Client) SubscribeAgent(name string) {
	c.agents[name] = struct{}{}
	c.hub.register <- c
}

// UnsubscribeAgent removes an agent's DAG stream from this client's subscription
// set and notifies the hub.
func (c *Client) UnsubscribeAgent(name string) {
	delete(c.agents, name)
	c.hub.unsubscribeAgent <- agentUnsubscribeMsg{client: c, agent: name}
}

// writePump pumps messages from the hub to the WebSocket connection.
func (c *Client) writePump() {
	defer func() {
		c.conn.Close(websocket.StatusGoingAway, "write pump closed")
	}()

	for data := range c.send {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := c.conn.Write(ctx, websocket.MessageText, data)
		cancel()
		if err != nil {
			return
		}
	}
}

// readPump reads messages from the WebSocket and handles subscribe/unsubscribe actions.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close(websocket.StatusNormalClosure, "read pump closed")
	}()

	for {
		_, data, err := c.conn.Read(context.Background())
		if err != nil {
			return
		}

		var action struct {
			Action string `json:"action"`
			RoomID int64  `json:"room_id"`
			Agent  string `json:"agent"`
		}
		if err := json.Unmarshal(data, &action); err != nil {
			continue
		}

		switch action.Action {
		case "subscribe":
			if action.RoomID > 0 {
				c.Subscribe(action.RoomID)
				if c.onSubscribe != nil {
					c.onSubscribe(action.RoomID)
				}
			}
		case "unsubscribe":
			if action.RoomID > 0 {
				c.Unsubscribe(action.RoomID)
			}
		case "subscribe_agent":
			if action.Agent != "" {
				c.SubscribeAgent(action.Agent)
			}
		case "unsubscribe_agent":
			if action.Agent != "" {
				c.UnsubscribeAgent(action.Agent)
			}
		}
	}
}
