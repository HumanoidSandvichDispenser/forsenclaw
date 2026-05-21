// Package room defines the core types for Hearth's room system: actors, messages,
// rooms, RFCs, and the protocol/dispatcher interfaces that orchestrate agent
// invocation.
package room

import (
	"encoding/json"
	"fmt"
	"time"
)

// ActorType distinguishes between human users and agents.
type ActorType string

const (
	// ActorUser represents a human user.
	ActorUser ActorType = "user"
	// ActorAgent represents an LLM agent.
	ActorAgent ActorType = "agent"
	// ActorSystem represents the system itself (e.g. for system messages).
	ActorSystem ActorType = "system"
)

// Actor represents any entity that can participate in rooms, send messages,
// and be subject to clearance and permissions. Users and agents are unified
// under this type — the only structural difference is authentication.
type Actor struct {
	// ID is a globally unique identifier: "user:<name>" or "agent:<name>".
	ID string `json:"id"`

	// Type distinguishes users from agents.
	Type ActorType `json:"type"`

	// Clearance is the actor's data classification tier. Higher numbers mean
	// more trust and access to sensitive data.
	Clearance int `json:"clearance"`

	// Name is the human-readable display name.
	Name string `json:"name"`
}

// Validate checks that the actor is well-formed. It returns an error if the
// ID is empty, the type is invalid, or the clearance is negative.
func (a Actor) Validate() error {
	if a.ID == "" {
		return fmt.Errorf("actor ID is required")
	}
	if a.Type != ActorUser && a.Type != ActorAgent && a.Type != ActorSystem {
		return fmt.Errorf("invalid actor type: %q", a.Type)
	}
	if a.Clearance < 0 {
		return fmt.Errorf("actor clearance must be non-negative, got %d", a.Clearance)
	}
	return nil
}

// IsUser returns true if this actor is a user.
func (a Actor) IsUser() bool { return a.Type == ActorUser }

// IsAgent returns true if this actor is an agent.
func (a Actor) IsAgent() bool { return a.Type == ActorAgent }

// MessageType categorises messages in a room transcript.
type MessageType string

const (
	// MessageText is a normal message sent by a participant.
	MessageText MessageType = "message"
	// MessageSystem is a system-generated message (e.g. room created, turn limit reached).
	MessageSystem MessageType = "system"
	// MessageToolCall records an agent turn that included one or more tool calls.
	// For native tool calling mode, ToolCalls carries the structured call data.
	// For XML mode, Content contains the raw response including <tool_call> blocks.
	MessageToolCall MessageType = "tool_call"
	// MessageToolResult records the result of a single tool invocation.
	// For native mode, ToolCallID correlates it with a MessageToolCall entry.
	// For XML mode, Content carries the <tool_response> XML.
	MessageToolResult MessageType = "tool_result"
)

// ToolCallRecord stores a single tool invocation in the transcript so that
// native tool-call history can be reconstructed for subsequent RFC processing.
type ToolCallRecord struct {
	// ID is the provider-assigned tool call ID. Empty for XML-mode calls.
	ID string `json:"id,omitempty"`
	// ToolName is the tool identifier.
	ToolName string `json:"tool_name"`
	// Arguments is the JSON-encoded parameter map.
	Arguments string `json:"arguments,omitempty"`
}

// Usage records token consumption for a message-producing inference call.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Message is a single record in a room transcript. Messages are immutable
// once written to the JSONL file.
type Message struct {
	// ID is a unique message identifier (UUID).
	ID string `json:"id"`

	// Timestamp is when the message was created.
	Timestamp time.Time `json:"timestamp"`

	// RoomID identifies the room this message belongs to.
	RoomID string `json:"room_id"`

	// Sender is the actor who produced this message.
	Sender Actor `json:"sender"`

	// ClearanceTag is the data classification of this message. It defaults to
	// the sender's clearance capped at the room's clearance ceiling.
	ClearanceTag int `json:"clearance_tag"`

	// Type distinguishes user/agent messages from system events.
	Type MessageType `json:"type"`

	// Content is the message body.
	Content string `json:"content"`

	// Usage records token consumption for agent responses.
	Usage *Usage `json:"usage,omitempty"`

	// ToolCalls carries structured tool call records for MessageToolCall messages
	// in native tool-calling mode. Empty for XML-mode tool call messages.
	ToolCalls []ToolCallRecord `json:"tool_calls,omitempty"`

	// ToolCallID correlates a MessageToolResult with its MessageToolCall (native mode).
	ToolCallID string `json:"tool_call_id,omitempty"`

	// ToolName is the tool identifier for MessageToolResult messages.
	ToolName string `json:"tool_name,omitempty"`
}

// Validate checks that the message is well-formed.
func (m Message) Validate() error {
	if m.ID == "" {
		return fmt.Errorf("message ID is required")
	}
	if m.RoomID == "" {
		return fmt.Errorf("message room_id is required")
	}
	if err := m.Sender.Validate(); err != nil {
		return fmt.Errorf("sender: %w", err)
	}
	if m.Type != MessageText && m.Type != MessageSystem &&
		m.Type != MessageToolCall && m.Type != MessageToolResult {
		return fmt.Errorf("invalid message type: %q", m.Type)
	}
	// MessageToolCall may have empty content (native mode with no prose).
	if m.Content == "" && m.Type != MessageToolCall {
		return fmt.Errorf("message content is required")
	}
	return nil
}

// ProtocolType names the behavioural contract governing a room.
type ProtocolType string

const (
	// ProtocolFreeForm is a two-participant reactive protocol. When A sends a
	// message, an RFC is issued to B.
	ProtocolFreeForm ProtocolType = "freeform"
)

// Room is a transcript container with participants, a clearance ceiling, and
// a protocol that governs how agents are invoked.
type Room struct {
	// ID is a unique room identifier (UUID).
	ID string `json:"id"`

	// Name is a human-readable name for the room (optional).
	Name string `json:"name"`

	// Participants are the actors currently in the room.
	Participants []Actor `json:"participants"`

	// ClearanceCeiling is the highest clearance tier any message may carry.
	ClearanceCeiling int `json:"clearance_ceiling"`

	// ProtocolType names the protocol governing this room.
	ProtocolType ProtocolType `json:"protocol_type"`

	// ProtocolConfig holds protocol-specific configuration (e.g. max_turns).
	ProtocolConfig json.RawMessage `json:"protocol_config,omitempty"`

	// ProtocolState holds serialised protocol runtime state for persistence.
	ProtocolState json.RawMessage `json:"protocol_state,omitempty"`

	// CreatedAt is when the room was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the room was last modified.
	UpdatedAt time.Time `json:"updated_at"`
}

// Validate checks that the room is well-formed.
func (r Room) Validate() error {
	if r.ID == "" {
		return fmt.Errorf("room ID is required")
	}
	if len(r.Participants) == 0 {
		return fmt.Errorf("room must have at least one participant")
	}
	if r.ClearanceCeiling < 0 {
		return fmt.Errorf("room clearance_ceiling must be non-negative")
	}
	if r.ProtocolType != ProtocolFreeForm {
		return fmt.Errorf("unsupported protocol type: %q", r.ProtocolType)
	}
	for i, p := range r.Participants {
		if err := p.Validate(); err != nil {
			return fmt.Errorf("participant %d: %w", i, err)
		}
		if p.Clearance > r.ClearanceCeiling {
			return fmt.Errorf("participant %q clearance %d exceeds room ceiling %d",
				p.ID, p.Clearance, r.ClearanceCeiling)
		}
	}
	return nil
}

// AgentParticipant returns the first agent participant in the room, or nil if
// none exists. Useful for FreeForm rooms where there is exactly one agent.
func (r Room) AgentParticipant() *Actor {
	for i := range r.Participants {
		if r.Participants[i].IsAgent() {
			return &r.Participants[i]
		}
	}
	return nil
}

// UserParticipant returns the first user participant in the room, or nil if
// none exists. Useful for FreeForm rooms where there is exactly one user.
func (r Room) UserParticipant() *Actor {
	for i := range r.Participants {
		if r.Participants[i].IsUser() {
			return &r.Participants[i]
		}
	}
	return nil
}

// ParticipantByID looks up a participant by their actor ID.
func (r Room) ParticipantByID(id string) *Actor {
	for i := range r.Participants {
		if r.Participants[i].ID == id {
			return &r.Participants[i]
		}
	}
	return nil
}

// RFC (Request for Comment) is the universal invocation primitive for agents.
// A protocol issues an RFC when it wants an agent to produce a response.
type RFC struct {
	// ID is a unique RFC identifier (UUID).
	ID string `json:"id"`

	// RoomID identifies the target room.
	RoomID string `json:"room_id"`

	// Target is the actor ID of the agent being invoked.
	Target string `json:"target"`

	// Payload carries the messages and metadata the agent should respond to.
	Payload RFCPayload `json:"payload"`

	// Deadline is an optional timeout for this RFC.
	Deadline time.Time `json:"deadline,omitempty"`
}

// RFCPayload is the content of an RFC — the messages an agent should respond to.
type RFCPayload struct {
	// Messages are the messages the agent should respond to (the "task").
	Messages []Message `json:"messages"`

	// Interjections are messages that arrived while another agent was responding.
	// They are included in the next RFC so the agent sees them.
	Interjections []Message `json:"interjections,omitempty"`

	// Metadata is protocol-specific state (iteration count, round number, etc.).
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ProtocolState is a serialised snapshot of protocol runtime state for
// persistence in SQLite.
type ProtocolState struct {
	// Type names the protocol that produced this state.
	Type string `json:"type"`

	// State is the protocol-specific JSON blob.
	State json.RawMessage `json:"state"`
}

// CompactionCursor tracks how far an agent has compacted a room's transcript.
type CompactionCursor struct {
	// AgentName is the agent this cursor belongs to.
	AgentName string `json:"agent_name"`

	// RoomID is the room this cursor tracks.
	RoomID string `json:"room_id"`

	// Offset is the number of messages already compacted (0-based). Messages
	// before this index are excluded from context assembly.
	Offset int `json:"offset"`

	// UpdatedAt is when the cursor was last modified.
	UpdatedAt time.Time `json:"updated_at"`
}

// Protocol is the behavioural contract that actively orchestrates agent
// invocation. Protocols decide when agents speak by issuing RFCs via the
// Dispatcher.
type Protocol interface {
	// Start is called when the room is created or the protocol is activated.
	// The protocol begins issuing RFCs according to its logic.
	Start(room *Room, dispatcher Dispatcher) error

	// OnMessage is called when a message lands in the room (from a user or
	// from an agent responding to an RFC). The protocol decides what to do
	// next: issue another RFC, terminate, wait, etc.
	OnMessage(room *Room, sender Actor, msg Message) error

	// OnRFCResponse is called when an agent completes its RFC response.
	// The protocol may issue the next RFC, collect for broadcast
	// reconciliation, or terminate.
	OnRFCResponse(room *Room, rfc RFC, response Message) error

	// OnInterjection is called when a non-targeted actor sends a message
	// (e.g. user typing during an agent response). The protocol decides
	// whether to queue it for the next RFC, pause, or ignore.
	OnInterjection(room *Room, sender Actor, msg Message) error

	// ShouldTerminate returns true if the protocol's end condition is met.
	ShouldTerminate(room *Room) bool

	// State returns a serialisable snapshot of protocol state for persistence.
	State() ProtocolState

	// Restore reconstructs protocol state from a persisted snapshot.
	Restore(state ProtocolState) error
}

// Dispatcher is how protocols issue RFCs to agents.
type Dispatcher interface {
	// IssueRFC sends an RFC to a specific agent.
	IssueRFC(rfc RFC) error

	// BroadcastRFC sends an RFC to all agent participants in a room simultaneously.
	BroadcastRFC(room *Room, payload RFCPayload) error
}

// FreeFormConfig is the configuration for FreeForm rooms.
type FreeFormConfig struct {
	// MaxTurns is the hard limit on agent-to-agent exchanges without user
	// participation. A value of 0 means no limit.
	MaxTurns int `json:"max_turns"`
}

// DefaultFreeFormConfig returns the default configuration for FreeForm rooms.
func DefaultFreeFormConfig() FreeFormConfig {
	return FreeFormConfig{MaxTurns: 20}
}

// ListOpts controls pagination and filtering for ListRooms.
type ListOpts struct {
	// Participant filters to rooms containing this actor ID.
	Participant string

	// Protocol filters to rooms with this protocol type.
	Protocol string

	// Limit is the maximum number of rooms to return.
	Limit int

	// Offset is the number of rooms to skip.
	Offset int
}
