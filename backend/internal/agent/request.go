package agent

import "time"

// Request is the universal invocation primitive for agents. Room protocols,
// the proactive system, and event triggers all deliver work through Requests.
type Request struct {
	// ID is a unique identifier for this request.
	ID string

	// Target is the name of the agent being invoked.
	Target string

	// Source identifies what issued this request.
	Source RequestSource

	// Payload carries the messages and metadata the agent should respond to.
	Payload RequestPayload

	// Deadline is an optional timeout. Zero means no deadline.
	Deadline time.Time
}

// RequestSource identifies what issued a request.
type RequestSource string

const (
	SourceRoom   RequestSource = "room"
	SourceSystem RequestSource = "system"
	SourceEvent  RequestSource = "event"
)

// RequestPayload is the content of a request.
type RequestPayload struct {
	// RoomID is the room this request originates from. Zero for system/event requests.
	RoomID int64

	// Messages are the messages the agent should respond to.
	Messages []Message

	// Metadata is protocol-specific state.
	Metadata map[string]any
}
