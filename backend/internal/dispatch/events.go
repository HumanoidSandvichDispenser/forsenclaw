package dispatch

// StreamEvent is a real-time event pushed to WebSocket clients.
type StreamEvent struct {
	Type    string `json:"type"`
	Payload any    `json:"payload,omitempty"`
}
