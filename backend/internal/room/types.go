// Package room defines the core types for Hearth's room system: actors,
// messages, rooms, and the store interface for persistence.
package room

import (
	"fmt"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Actor
// ---------------------------------------------------------------------------

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

// Actor IDs follow a "<type>:<name>" convention (e.g. "agent:forsen"). These
// helpers build and parse that scheme so the prefix strings live in one place.

// ActorID builds the canonical "<type>:<name>" identifier.
func ActorID(t ActorType, name string) string {
	return string(t) + ":" + name
}

// AgentID returns the canonical ID for an agent name ("agent:<name>").
func AgentID(name string) string { return ActorID(ActorAgent, name) }

// UserID returns the canonical ID for a user name ("user:<name>").
func UserID(name string) string { return ActorID(ActorUser, name) }

// SplitActorID splits "<type>:<name>" into its type and name. ok is false when
// the id lacks a non-empty type prefix or a non-empty name.
func SplitActorID(id string) (typ ActorType, name string, ok bool) {
	i := strings.IndexByte(id, ':')
	if i <= 0 || i == len(id)-1 {
		return "", "", false
	}
	return ActorType(id[:i]), id[i+1:], true
}

// ---------------------------------------------------------------------------
// Message
// ---------------------------------------------------------------------------

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
// native tool-call history can be reconstructed for subsequent inference calls.
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
	// CachedTokens is the portion of InputTokens served from the provider's
	// prompt cache. Zero when no cache hit was reported.
	CachedTokens int `json:"cached_tokens"`
}

// Message is a single record in a room transcript. Messages are immutable
// once written to the database.
type Message struct {
	// ID is the globally unique message identifier, assigned by the database.
	ID int64 `json:"id" gorm:"primaryKey;autoIncrement"`

	// RoomID identifies the room this message belongs to.
	RoomID int64 `json:"room_id" gorm:"not null;index"`

	// ParentID is the ID of the parent message in the conversation tree.
	// NULL for the first message in a room.
	ParentID *int64 `json:"parent_id,omitempty" gorm:"index"`

	// Timestamp is when the message was created.
	Timestamp time.Time `json:"timestamp"`

	// Sender is the actor who produced this message.
	Sender Actor `json:"sender" gorm:"serializer:json"`

	// ClearanceTag is the data classification of this message. It defaults to
	// min(sender.Clearance, room.Clearance) at write time.
	ClearanceTag int `json:"clearance_tag"`

	// Type distinguishes user/agent messages from system events.
	Type MessageType `json:"type"`

	// Content is the message body.
	Content string `json:"content"`

	// UsageInputTokens records input token consumption for agent responses.
	UsageInputTokens int `json:"usage_input_tokens" gorm:"default:0"`

	// UsageOutputTokens records output token consumption for agent responses.
	UsageOutputTokens int `json:"usage_output_tokens" gorm:"default:0"`

	// UsageCachedTokens records how many input tokens were served from the
	// provider's prompt cache for this response.
	UsageCachedTokens int `json:"usage_cached_tokens" gorm:"default:0"`

	// ToolCalls carries structured tool call records for MessageToolCall messages
	// in native tool-calling mode. Empty for XML-mode tool call messages.
	ToolCalls []ToolCallRecord `json:"tool_calls,omitempty" gorm:"serializer:json"`

	// ToolCallID correlates a MessageToolResult with its MessageToolCall (native mode).
	ToolCallID string `json:"tool_call_id,omitempty" gorm:"default:''"`

	// ToolName is the tool identifier for MessageToolResult messages.
	ToolName string `json:"tool_name,omitempty" gorm:"default:''"`
}

type MessageDelta struct {
	RoomID int64  `json:"room_id"`
	Actor  Actor  `json:"actor"`
	Delta  string `json:"delta"`
}

// Usage returns the token usage as a pointer, or nil if both counts are zero.
func (m Message) Usage() *Usage {
	if m.UsageInputTokens == 0 && m.UsageOutputTokens == 0 {
		return nil
	}
	return &Usage{
		InputTokens:  m.UsageInputTokens,
		OutputTokens: m.UsageOutputTokens,
		CachedTokens: m.UsageCachedTokens,
	}
}

// Validate checks that the message is well-formed.
func (m Message) Validate() error {
	if m.RoomID <= 0 {
		return fmt.Errorf("message room_id must be positive, got %d", m.RoomID)
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

// ---------------------------------------------------------------------------
// Room
// ---------------------------------------------------------------------------

// Room is a clearance-bounded conversation space. Clearance is the
// primary structural boundary: it determines which memory strata agents
// assemble, how messages are classified at write time, and what information
// can flow in or out of the room. Rooms are context isolation units, not
// protocol containers.
type Room struct {
	// ID is a unique room identifier (auto-incremented integer).
	ID int64 `json:"id" gorm:"primaryKey;autoIncrement"`

	// Head is the ID of the tip message on the active branch. NULL if the room
	// has no messages yet.
	Head *int64 `json:"head,omitempty"`

	// Name is a human-readable name for the room (optional).
	Name string `json:"name"`

	// Participants are the actors currently in the room.
	Participants []Actor `json:"participants" gorm:"-"`

	// Clearance is the room's data classification tier. It determines which
	// memory strata agents assemble and how messages are classified at write
	// time. The effective clearance for an agent is min(agent.Clearance, room.Clearance).
	Clearance int `json:"clearance"`

	// CreatedAt is when the room was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the room was last modified.
	UpdatedAt time.Time `json:"updated_at"`
}

// Validate checks that the room is well-formed.
func (r Room) Validate() error {
	if r.ID < 0 {
		return fmt.Errorf("room ID must be non-negative")
	}
	if len(r.Participants) == 0 {
		return fmt.Errorf("room must have at least one participant")
	}
	if r.Clearance < 0 {
		return fmt.Errorf("room clearance must be non-negative")
	}
	for i, p := range r.Participants {
		if err := p.Validate(); err != nil {
			return fmt.Errorf("participant %d: %w", i, err)
		}
		if p.Clearance > r.Clearance {
			return fmt.Errorf("participant %q clearance %d exceeds room clearance %d",
				p.ID, p.Clearance, r.Clearance)
		}
	}
	return nil
}

// AgentParticipant returns the first agent participant in the room, or nil.
func (r Room) AgentParticipant() *Actor {
	for i := range r.Participants {
		if r.Participants[i].IsAgent() {
			return &r.Participants[i]
		}
	}
	return nil
}

// UserParticipant returns the first user participant in the room, or nil.
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
