package agent

import "time"

// MessageType categorises messages in a transcript.
type MessageType string

const (
	MessageText       MessageType = "text"
	MessageToolCall   MessageType = "tool_call"
	MessageToolResult MessageType = "tool_result"
)

// Message is a single entry in a conversation.
type Message struct {
	Sender    string
	Content   string
	Timestamp time.Time
	Type      MessageType
}
