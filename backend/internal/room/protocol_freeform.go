package room

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// FreeFormProtocol implements the Protocol interface for two-participant rooms.
// When participant A sends a message, the protocol issues an RFC to participant B.
// This is the simplest protocol — suitable for DMs and pair conversations.
type FreeFormProtocol struct {
	mu         sync.Mutex
	config     FreeFormConfig
	room       *Room
	turnCount  int
	dispatcher Dispatcher
}

// NewFreeFormProtocol creates a FreeForm protocol for the given room. It parses
// the room's ProtocolConfig into FreeFormConfig. If parsing fails, an error is
// returned.
func NewFreeFormProtocol(room *Room) (*FreeFormProtocol, error) {
	if room == nil {
		return nil, fmt.Errorf("room is nil")
	}
	if len(room.Participants) != 2 {
		return nil, fmt.Errorf("freeform requires exactly 2 participants, got %d", len(room.Participants))
	}

	config := DefaultFreeFormConfig()
	if len(room.ProtocolConfig) > 0 {
		if err := json.Unmarshal(room.ProtocolConfig, &config); err != nil {
			return nil, fmt.Errorf("parse freeform config: %w", err)
		}
	}

	return &FreeFormProtocol{
		config: config,
		room:   room,
	}, nil
}

// Start is called when the room is created. For FreeForm, this is a no-op
// because the protocol is purely reactive — it waits for messages.
func (p *FreeFormProtocol) Start(room *Room, dispatcher Dispatcher) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.room = room
	p.dispatcher = dispatcher
	return nil
}

// OnMessage is called when a message lands in the room. If the sender is one
// participant, an RFC is issued to the other participant.
func (p *FreeFormProtocol) OnMessage(room *Room, sender Actor, msg Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.shouldTerminateLocked() {
		return fmt.Errorf("protocol has terminated (turn limit %d reached)", p.config.MaxTurns)
	}

	// Verify sender is a participant, then find the other participant.
	senderFound := false
	var target *Actor
	for i := range room.Participants {
		if room.Participants[i].ID == sender.ID {
			senderFound = true
		} else if target == nil {
			target = &room.Participants[i]
		}
	}
	if !senderFound {
		return fmt.Errorf("sender %q is not a participant in room %q", sender.ID, room.ID)
	}
	if target == nil {
		return fmt.Errorf("no other participant found in room %q", room.ID)
	}

	// Only issue RFCs to agents. If the target is a user, do nothing — users
	// respond on their own schedule.
	if !target.IsAgent() {
		return nil
	}

	// Build the RFC payload
	payload := RFCPayload{
		Messages: []Message{msg},
		Metadata: map[string]any{
			"turn_count": p.turnCount,
		},
	}

	rfc := RFC{
		ID:      generateRFCID(),
		RoomID:  room.ID,
		Target:  target.ID,
		Payload: payload,
	}

	if p.dispatcher == nil {
		return fmt.Errorf("dispatcher not set")
	}
	return p.dispatcher.IssueRFC(rfc)
}

// OnRFCResponse is called when an agent completes its RFC response. For
// FreeForm, this increments the turn count and then waits for the next user
// message.
func (p *FreeFormProtocol) OnRFCResponse(room *Room, rfc RFC, response Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.turnCount++
	return nil
}

// OnInterjection is called when a non-targeted actor sends a message while an
// agent is responding. For FreeForm, interjections are handled by the
// dispatcher's queueing mechanism; the protocol simply acknowledges them.
func (p *FreeFormProtocol) OnInterjection(room *Room, sender Actor, msg Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return nil
}

// ShouldTerminate returns true if the turn limit has been reached.
func (p *FreeFormProtocol) ShouldTerminate(room *Room) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.shouldTerminateLocked()
}

// shouldTerminateLocked is the internal version that assumes the caller holds p.mu.
func (p *FreeFormProtocol) shouldTerminateLocked() bool {
	if p.config.MaxTurns <= 0 {
		return false
	}
	return p.turnCount >= p.config.MaxTurns
}

// State returns serialisable protocol state.
func (p *FreeFormProtocol) State() ProtocolState {
	p.mu.Lock()
	defer p.mu.Unlock()

	state := struct {
		TurnCount int `json:"turn_count"`
	}{
		TurnCount: p.turnCount,
	}
	data, _ := json.Marshal(state)

	return ProtocolState{
		Type:  string(ProtocolFreeForm),
		State: data,
	}
}

// Restore reconstructs protocol state from a persisted snapshot.
func (p *FreeFormProtocol) Restore(state ProtocolState) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if state.Type != string(ProtocolFreeForm) {
		return fmt.Errorf("expected protocol type %q, got %q", ProtocolFreeForm, state.Type)
	}

	var s struct {
		TurnCount int `json:"turn_count"`
	}
	if err := json.Unmarshal(state.State, &s); err != nil {
		return fmt.Errorf("unmarshal state: %w", err)
	}

	p.turnCount = s.TurnCount
	return nil
}

// SetDispatcher sets the dispatcher reference. This is primarily used by
// tests; in production the dispatcher is set via Start.
func (p *FreeFormProtocol) SetDispatcher(d Dispatcher) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dispatcher = d
}

// generateRFCID returns a UUID-based RFC identifier, safe for concurrent use.
func generateRFCID() string {
	return "rfc_" + uuid.New().String()
}
