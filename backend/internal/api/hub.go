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
func (n *HubNotifier) NotifyConfirmationPending(roomID string, c agent.PendingConfirmation) {
	n.hub.Broadcast(roomID, dispatch.StreamEvent{
		Type:    "confirmation.pending",
		Payload: c,
	})
}

// Hub manages WebSocket client connections and broadcasts room events.
type Hub struct {
	mu              sync.RWMutex
	rooms           map[string]map[*Client]struct{} // room_id -> set of clients
	register        chan *Client
	unregister      chan *Client
	unsubscribeRoom chan roomUnsubscribeMsg
	broadcast       chan broadcastMsg
}

type broadcastMsg struct {
	roomID string
	data   []byte
}

type roomUnsubscribeMsg struct {
	client *Client
	roomID string
}

// NewHub creates a new WebSocket hub.
func NewHub() *Hub {
	return &Hub{
		rooms:           make(map[string]map[*Client]struct{}),
		register:        make(chan *Client),
		unregister:      make(chan *Client),
		unsubscribeRoom: make(chan roomUnsubscribeMsg),
		broadcast:       make(chan broadcastMsg, 64),
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

		case msg := <-h.broadcast:
			h.broadcastToRoom(msg.roomID, msg.data)
		}
	}
}

// Broadcast sends a real-time event to all clients subscribed to the given room.
func (h *Hub) Broadcast(roomID string, event dispatch.StreamEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("hub: failed to marshal event: %v", err)
		return
	}

	select {
	case h.broadcast <- broadcastMsg{roomID: roomID, data: data}:
	default:
		log.Printf("hub: broadcast channel full, dropping event for room %s", roomID)
	}
}

// registerClient adds a client to its subscribed rooms.
func (h *Hub) registerClient(client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for roomID := range client.rooms {
		if h.rooms[roomID] == nil {
			h.rooms[roomID] = make(map[*Client]struct{})
		}
		h.rooms[roomID][client] = struct{}{}
	}
}

// unsubscribeClientFromRoom removes a single client from one room in the hub map.
func (h *Hub) unsubscribeClientFromRoom(client *Client, roomID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if clients, ok := h.rooms[roomID]; ok {
		delete(clients, client)
		if len(clients) == 0 {
			delete(h.rooms, roomID)
		}
	}
}

// unregisterClient removes a client from all rooms and closes its send channel.
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
	close(client.send)
}

// broadcastToRoom fans out a message to all clients subscribed to a room.
func (h *Hub) broadcastToRoom(roomID string, data []byte) {
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

// Client represents a single WebSocket connection.
type Client struct {
	hub   *Hub
	conn  *websocket.Conn
	rooms map[string]struct{} // subscribed room IDs
	send  chan []byte

	// onSubscribe is called after a client successfully subscribes to a room.
	// Used to replay pending state (e.g. confirmation events) to newly connected clients.
	onSubscribe func(roomID string)
}

// NewClient creates a new client for the given WebSocket connection.
func NewClient(hub *Hub, conn *websocket.Conn) *Client {
	return &Client{
		hub:   hub,
		conn:  conn,
		rooms: make(map[string]struct{}),
		send:  make(chan []byte, 256),
	}
}

// Subscribe adds a room to this client's subscription set.
func (c *Client) Subscribe(roomID string) {
	c.rooms[roomID] = struct{}{}
	c.hub.register <- c
}

// Unsubscribe removes a room from this client's subscription set and notifies
// the hub so it stops delivering events for that room to this client.
func (c *Client) Unsubscribe(roomID string) {
	delete(c.rooms, roomID)
	c.hub.unsubscribeRoom <- roomUnsubscribeMsg{client: c, roomID: roomID}
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
			RoomID string `json:"room_id"`
		}
		if err := json.Unmarshal(data, &action); err != nil {
			continue
		}

		switch action.Action {
		case "subscribe":
			if action.RoomID != "" {
				c.Subscribe(action.RoomID)
				if c.onSubscribe != nil {
					c.onSubscribe(action.RoomID)
				}
			}
		case "unsubscribe":
			if action.RoomID != "" {
				c.Unsubscribe(action.RoomID)
			}
		}
	}
}
