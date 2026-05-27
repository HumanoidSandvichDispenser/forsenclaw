package agent

import "sync"

// PendingConfirmation holds the data the frontend needs to render a confirmation UI.
type PendingConfirmation struct {
	NodeID    string `json:"node_id"`
	AgentName string `json:"agent_name"`
	RoomID    int64  `json:"room_id"`
	ToolName  string `json:"tool_name"`
	Args      string `json:"args"` // JSON-encoded arguments
}

// ConfirmationNotifier is implemented by the API layer to push real-time
// confirmation events to WebSocket clients. Defined here to avoid an import
// cycle (api imports agent, not the other way around).
type ConfirmationNotifier interface {
	NotifyConfirmationPending(roomID int64, c PendingConfirmation)
}

// ConfirmationRegistry tracks all currently pending confirmations in memory,
// keyed by room ID. It is the source of truth for both the REST endpoint
// (on-load) and the WebSocket subscribe replay (real-time catch-up).
type ConfirmationRegistry struct {
	mu      sync.RWMutex
	pending map[int64][]PendingConfirmation // roomID → confirmations
}

// NewConfirmationRegistry creates an empty registry.
func NewConfirmationRegistry() *ConfirmationRegistry {
	return &ConfirmationRegistry{pending: make(map[int64][]PendingConfirmation)}
}

// Register adds a pending confirmation. Called by ConfirmationHandler.Handle.
func (r *ConfirmationRegistry) Register(c PendingConfirmation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending[c.RoomID] = append(r.pending[c.RoomID], c)
}

// Deregister removes a confirmation by node ID once it has been resolved or cancelled.
func (r *ConfirmationRegistry) Deregister(roomID int64, nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	confs := r.pending[roomID]
	for i, c := range confs {
		if c.NodeID == nodeID {
			r.pending[roomID] = append(confs[:i], confs[i+1:]...)
			if len(r.pending[roomID]) == 0 {
				delete(r.pending, roomID)
			}
			return
		}
	}
}

// List returns a snapshot of all pending confirmations for a room.
func (r *ConfirmationRegistry) List(roomID int64) []PendingConfirmation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	confs := r.pending[roomID]
	if len(confs) == 0 {
		return nil
	}
	out := make([]PendingConfirmation, len(confs))
	copy(out, confs)
	return out
}

// Get returns the pending confirmation with the given node ID within a room, or nil.
func (r *ConfirmationRegistry) Get(roomID int64, nodeID string) *PendingConfirmation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.pending[roomID] {
		if c.NodeID == nodeID {
			cp := c
			return &cp
		}
	}
	return nil
}

// Take atomically gets and removes a pending confirmation. Returns nil if not found.
// Use this in the respond endpoint instead of Get+Deregister to prevent two concurrent
// requests from both resolving the same confirmation.
func (r *ConfirmationRegistry) Take(roomID int64, nodeID string) *PendingConfirmation {
	r.mu.Lock()
	defer r.mu.Unlock()
	confs := r.pending[roomID]
	for i, c := range confs {
		if c.NodeID == nodeID {
			r.pending[roomID] = append(confs[:i], confs[i+1:]...)
			if len(r.pending[roomID]) == 0 {
				delete(r.pending, roomID)
			}
			cp := c
			return &cp
		}
	}
	return nil
}
